# Try the decision service on a Mac

This guide installs the [decision service](./supervisor.md) on one Mac and
walks one approval from the agent's request to the password of the
approver. It uses two accounts: your own account, which runs the agent,
and a second account, which approves. Use a test Mac or a virtual machine.
The install changes system files for every user of the host.

For a fleet, the [managed install guide](./mdm.md) has the contract and
the Jamf recipe.

## What you need

- macOS 13 or later, and an administrator account.
- Go and `make`, to build Gryph from source, or the package
  `gryph_<version>_darwin_all.pkg` from the release page.
- Fast User Switching, so the approver can log in to a desktop session.
  On macOS the password prompt shows only in a desktop session.

## 0. Check that the test names are free

The guide makes an account, a group and files with fixed names, and the
teardown removes them. Check that none of them exists before you start:

```sh
dscl . -read /Users/approver
dscl . -read /Groups/gryph-admins
ls -d "/Library/Application Support/safedep/gryph" /opt/safedep/gryph /var/root/gryph-test
```

Each command must report that the name does not exist. If an account or a
group exists, use another name in every step.

## 1. Build and install the package

The managed install accepts only a binary on a path that root owns from
the file up to `/`. A binary under the Homebrew prefix fails with exit 3,
because the admin user owns that prefix.

```sh
git clone https://github.com/safedep/gryph && cd gryph
make gryph
./scripts/macos-pkg.sh bin/gryph 0.0.0 /tmp/gryph.pkg
sudo installer -pkg /tmp/gryph.pkg -target /
```

With the release package, run only the `installer` command.

## 2. Make the approver account

A member of the admin group answers requests. Make the group and a second
account in it. `sysadminctl` asks for the password of the new account.

```sh
sudo dseditgroup -o create gryph-admins
sudo sysadminctl -addUser approver -password -
sudo dseditgroup -o edit -a approver -t user gryph-admins
```

Your own account must not be in the group. The account that asks can
never answer its own request.

## 3. Write the managed configuration and the policy

The input files must pass the same root-owned path check as the binary,
so keep them under the home of root.

```sh
sudo mkdir -p /var/root/gryph-test
sudo tee /var/root/gryph-test/managed.yml >/dev/null <<'YAML'
policy:
  enabled: true
  log_all_evaluations: true
  approval:
    channels: [same-user-tty, local-admin]
    min_assurance: local-admin
    inline_wait: 15s
    request_ttl: 30m
    max_grant_scope: session
    local_admin:
      group: gryph-admins
      allow_self_elevated: false
      allow_without_auth: false
managed:
  agents: [claude-code]
supervisor:
  enabled: true
YAML
sudo tee /var/root/gryph-test/policy.yaml >/dev/null <<'YAML'
version: "1"
rules:
  - id: deploy-needs-admin
    action: escalate
    severity: high
    match:
      action_types: [command_exec]
      command_patterns:
        - "make deploy"
    message: "a deploy needs an admin"
YAML
```

The [Linux guide](./supervisor-linux.md#3-write-the-managed-configuration-and-the-policy)
explains these keys.

## 4. Install

```sh
sudo /opt/safedep/gryph/bin/gryph install --managed \
  --config /var/root/gryph-test/managed.yml \
  --policy /var/root/gryph-test/policy.yaml --json
```

The install does these steps in order:

1. It writes the managed files to `/Library/Application Support/safedep/gryph/`.
2. It makes the hidden `_gryph` service account.
3. It writes the right `io.safedep.gryph.approve` into the authorization
   database and the LaunchDaemon
   `/Library/LaunchDaemons/io.safedep.gryph-supervisor.plist`.
4. It loads the job, and waits for the service to answer on
   `/var/db/safedep/gryph/hook.sock`.
5. It turns the switch on.

The report ends with `"status": "ok"`. macOS shows a "Background Items
Added" notice for the job.

## 5. Check the install

```sh
sudo /opt/safedep/gryph/bin/gryph doctor --managed
sudo launchctl print system/io.safedep.gryph-supervisor | head -20
security authorizationdb read io.safedep.gryph.approve
/opt/safedep/gryph/bin/gryph doctor
```

Expected:

- `gryph doctor --managed` reports the `locked` profile and
  `approvers same-user-tty, local-admin (group gryph-admins)`.
- `launchctl print` shows the job with `state = running`.
- `security authorizationdb read` prints the right, with `session-owner`
  and `shared` set to false.
- `gryph doctor` as your own account shows the row `Decision service`
  with `/var/db/safedep/gryph/hook.sock, profile enforce`.

## 6. Make a request as the developer

As your own account, ask the agent to run `make deploy`, or send the hook
event by hand:

```sh
cat > /tmp/deploy.json <<'JSON'
{"session_id": "test-1", "cwd": "/tmp", "hook_event_name": "PreToolUse",
 "tool_name": "Bash", "tool_input": {"command": "make deploy"},
 "tool_use_id": "t1"}
JSON
/opt/safedep/gryph/bin/gryph _hook claude-code PreToolUse < /tmp/deploy.json
```

The command exits non-zero and names a pending request. Note the request
ID.

## 7. Answer as the approver

Switch to the `approver` account with Fast User Switching, and open
Terminal in that desktop session:

```sh
/opt/safedep/gryph/bin/gryph policy approve list
/opt/safedep/gryph/bin/gryph policy approve resolve --id 7f3c2a1b --decision allow --scope session --note "test"
```

`resolve` asks for a confirmation on the terminal. Then macOS shows a
password prompt in the desktop session. Enter the password of `approver`:

```
Answer request 7f3c2a1b with ALLOW? [y/N]: y
Request 7f3c2a1b is approved (local-auth), grant 4c1d9e20
```

`local-auth` is the assurance: the password proves a person at the
keyboard.

## 8. Check what the service refuses

Make a new request (step 6), then try each answer below. Each one leaves
the request pending.

| Try | Result |
|---|---|
| Cancel at the password prompt | `the answer needs the password of the approver: no password was given: ...` |
| `ssh approver@localhost`, then `resolve` | the same: SSH has no desktop session to show the prompt |
| `resolve` as your own account | `the account that asked cannot answer its own request` |
| `su - approver` from your own terminal, then `resolve` | `the answer comes from the login session that asked (self-elevated), ...` |

## 9. Retry as the developer

Send the hook event of step 6 again. The command passes on the session
grant. `gryph policy approve history --format json` shows the channel, the
assurance, the approver and the grant.

## 10. Change the policy

Edit `/var/root/gryph-test/policy.yaml` and run the install command of step
4 again. The install writes the difference and reloads the service. To
reload by hand:

```sh
sudo launchctl kill SIGHUP system/io.safedep.gryph-supervisor
```

## When something does not work

```sh
sudo tail -f /var/log/safedep/gryph-supervisor.log
sudo launchctl print system/io.safedep.gryph-supervisor
sudo /opt/safedep/gryph/bin/gryph doctor --managed --json
```

| Symptom | Cause |
|---|---|
| The install exits 3 with a chain error | A file or a directory above the binary or the input files is not owned by root, or is writable by group or other. |
| The service does not answer, and the switch stays off | launchd did not start the job. Read the log and `launchctl print`. |
| `... no password was given: ...` | `resolve` ran outside a desktop session, or the prompt was cancelled. |
| `resolve` reports `(local-admin)`, not `(local-auth)` | The right is not in the authorization database. Run the install again. |
| The job is off in System Settings, Login Items | A user turned it off. Deploy the Managed Login Items profile of the [managed install guide](./mdm.md#jamf-pro-macos). |

## Remove the test

These commands remove only what this guide made, if step 0 found none of
the names.

```sh
sudo /opt/safedep/gryph/bin/gryph uninstall --managed --purge --json
sudo sysadminctl -deleteUser approver
sudo dseditgroup -o delete gryph-admins
sudo dscl . -delete /Users/_gryph
sudo dscl . -delete /Groups/_gryph
sudo rm -rf /var/root/gryph-test /opt/safedep/gryph
sudo pkgutil --forget io.safedep.gryph
```

`uninstall --managed` turns the switch off first, then removes the job,
the right, the hook entries and the managed files.
