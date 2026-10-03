# Managed install for a fleet

An administrator installs Gryph for every user of a host with three commands.
A mobile device management tool (MDM) runs them as root, or from an elevated
prompt on Windows, and reads one JSON report as a compliance attribute. This
guide is the contract of those commands and a recipe for each common tool.
The flags live in the [CLI reference](./cli-reference.md#managed-install).

## What a managed install gives

A managed install puts the hook entry of each agent, the policy and the
configuration out of the user's reach. `gryph doctor --managed` then reports
the `locked` profile: the hook entry and the policy files resist the non-admin
user and their agents. The decision still runs in a process of the user, and
the audit trail stays in the user's home, so the report also says "decision
and audit trail not protected" and "Key: user-owned (not protected)". The
[self-protection guide](./self-protection.md) and the
[threat model](./security-policy-threat-model.md) say what you can claim.

Root can always uninstall. Nothing in Gryph resists the administrator.

## The contract

| Step | Command | What it does |
|---|---|---|
| Configure and install | `gryph install --managed --config <file> [--policy <file>] [--trust-store <file>] --json` | Validates the whole input, then writes the managed configuration, the managed policy, the managed trust store, the managed hook entry of every agent in `managed.agents`, and the system-wide reconcile job. |
| Rotate the machine key | `gryph supervisor keys rotate --json` | Writes a new receipt signing key for the decision service, keeps the old public key in the trust store, and reloads the running service. Run it on a schedule or after an incident. |
| Report compliance | `gryph doctor --managed --json` | Prints the profile, the reasons that keep it from `locked`, the chain state of each managed file and of the binary, and one row per agent. Exit 0 for `locked`, 1 otherwise. |
| Remove | `gryph uninstall --managed [--purge] --json` | Removes the managed hook entries, the Gryph entries in every home, the reconcile job, and the managed files. Keeps the per-user state unless `--purge`. |

Rules that hold for every command:

- **Idempotent.** A second run with the same input changes nothing and exits
  0. A run with a new configuration applies the difference. An agent that
  leaves `managed.agents` loses its entry on the next run.
- **Exit codes.** 0 success, 1 failure, 3 invalid input with nothing changed,
  10 partial success. With 10 the JSON names the degraded agent or account.
- **No environment trust.** The commands read no `HOME`, `XDG_*`, `GRYPH_*`
  or `PROGRAMDATA` variable and run no program. Every path comes from the
  input file and the platform.
- **Trusted input only.** `--config`, `--policy` and `--trust-store` must
  pass the chain check: root owns the file and every directory above it, and
  nothing in the chain is writable by group or other. On Windows, `SYSTEM`,
  `Administrators` or `TrustedInstaller` own every component below
  `%ProgramData%` or `%ProgramFiles%`, with no access control entry that lets
  another principal write. A file in a user's home fails with exit 3.
- **The binary.** The hook entries name the `gryph` binary by an absolute
  path that passes the same check. Put it at the default of the platform, or
  set `managed.binary`.

| Platform | Binary | Managed directory |
|---|---|---|
| Linux | `/opt/safedep/gryph/bin/gryph`, with `/usr/bin/gryph` as a link for `PATH` | `/etc/safedep/gryph/` |
| macOS | `/opt/safedep/gryph/bin/gryph`, with `/usr/local/bin/gryph` as a link for `PATH` | `/Library/Application Support/safedep/gryph/` |
| Windows | `%ProgramFiles%\SafeDep\gryph\gryph.exe` | `%ProgramData%\safedep\gryph\` |

The managed directory holds `config.yml`, `policy.yaml`, `policies/` and
`keys/receipt-pub.json`. The install writes them. Do not write them by hand,
so the chain and the ownership stay right.

### The configuration file

```yaml
# managed.yml
policy:
  enabled: true
  allow_user_policy: false       # drop the user's own policy files
  self_protection:
    repair: true                 # the default under a managed configuration
managed:
  agents: [claude-code, codex, cursor, gemini, windsurf]
  lock_hooks: [claude-code]      # only managed hooks run, Claude Code and Codex only
  # binary: /opt/safedep/gryph/bin/gryph
```

The lock also stops the developer's own hooks, so it is off by default. The
managed hooks hold without it for the `locked` class. The
[coverage table](./agent-enforcement-coverage.md#managed-settings) says what
each agent documents.

### What root touches

The install writes the managed directory and the system files of the agents.
It never touches a home. The reconcile job runs as each user, from the
scheduler of that user, at login and every 15 minutes.

`gryph uninstall --managed` is the one command where a root process reaches
into a home, because the agent hook files of each user still name a binary
about to go away. It starts a helper for each account that drops to that
account's uid and gid before it opens a file, with the repair rules: no link in
the path, a temporary file and a rename, Gryph entries only. Root itself never
writes into a home.

### Reading the report

`gryph doctor --managed --json` is stable. `profile` is `locked` or `none`,
`issues` names what is missing, and every field is in the
[reference](./cli-reference.md#managed-doctor). A compliance script reads
`profile` and nothing else:

```bash
gryph doctor --managed --json | jq -r .profile
```

## Recipes

Each recipe does the same three things: place the binary, place the input
files, run the commands. Replace the download step with your artifact store.
On Linux the release ships a deb and an rpm package that put the binary at
`/opt/safedep/gryph/bin/gryph` with a `/usr/bin/gryph` link. The signed
installers for macOS and Windows are planned. Until they ship, the scripts
place the binary.

Every package and the checksum file carry a Sigstore signature from the
release workflow, with no long-lived key. Verify one before you ship it to a
fleet, with the certificate and the signature that sit next to it on the
release page:

```bash
cosign verify-blob \
  --certificate gryph_1.2.3_linux_amd64.deb.pem \
  --signature gryph_1.2.3_linux_amd64.deb.sig \
  --certificate-identity-regexp '^https://github.com/safedep/gryph/\.github/workflows/goreleaser\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  gryph_1.2.3_linux_amd64.deb
```

### Linux with a configuration management tool

Install the package, place the input files and run the install as root. Set
the two kernel settings that close the known same-user bypasses. `gryph
doctor` reports both under "Host posture".

```bash
apt install ./gryph_1.2.3_linux_amd64.deb     # or: rpm -i gryph-1.2.3.x86_64.rpm
install -d -m 0755 -o root -g root /etc/safedep/gryph-input
install -m 0644 -o root -g root ./managed.yml ./policy.yaml /etc/safedep/gryph-input/

cat > /etc/sysctl.d/60-gryph.conf <<'SYSCTL'
# An unprivileged user namespace lets a user bind-mount a file of their own
# over a managed settings file or over the binary.
kernel.apparmor_restrict_unprivileged_userns = 1
# On a kernel without that setting, use: user.max_user_namespaces = 0
# With scope 0, a process of the user can trace the hook and change its answer.
kernel.yama.ptrace_scope = 1
SYSCTL
sysctl --system

gryph install --managed --config /etc/safedep/gryph-input/managed.yml \
  --policy /etc/safedep/gryph-input/policy.yaml --json
```

A compliance check runs `gryph doctor --managed --json` and reads `profile`.
With a tool that tracks drift, run the install on every pass: it changes
nothing when the host is already right.

### Jamf Pro (macOS)

1. Ship the binary and the input files as a package that installs to
   `/opt/safedep/gryph/bin/gryph` (mode 0755, `root:wheel`) and
   `/opt/safedep/gryph/input/` (mode 0644, `root:wheel`). A package keeps
   the ownership right. A script that copies from a user's download does not.
2. Add a policy with a script that runs after the package:

   ```bash
   #!/bin/bash
   /opt/safedep/gryph/bin/gryph install --managed \
     --config /opt/safedep/gryph/input/managed.yml \
     --policy /opt/safedep/gryph/input/policy.yaml --json
   ```

   Run it at enrollment and on a recurring check-in. It changes nothing when
   the host is already right.
3. Add an extension attribute with this script. Smart groups and reports then
   see `locked` or `none`:

   ```bash
   #!/bin/bash
   profile=$(/opt/safedep/gryph/bin/gryph doctor --managed --json 2>/dev/null | /usr/bin/python3 -c 'import json,sys; print(json.load(sys.stdin)["profile"])' 2>/dev/null)
   echo "<result>${profile:-none}</result>"
   ```

4. To remove, run `/opt/safedep/gryph/bin/gryph uninstall --managed --json`
   from a policy, then remove the package.

The reconcile job is a launchd agent at
`/Library/LaunchAgents/io.safedep.gryph-reconcile.plist`. An account that is
logged in picks it up at its next login.

### Kandji (macOS)

1. Ship the binary and the input files with a Custom App, to the same paths
   as the Jamf recipe.
2. Add a Custom Script with the install command as the audit script, so it
   runs on every agent check-in. It changes nothing when the host is already
   right, and a non-zero exit shows up as a failed audit:

   ```bash
   #!/bin/bash
   /opt/safedep/gryph/bin/gryph install --managed \
     --config /opt/safedep/gryph/input/managed.yml \
     --policy /opt/safedep/gryph/input/policy.yaml --json
   ```

3. Add a second Custom Script whose audit script is the compliance check. It
   exits 0 only for `locked`:

   ```bash
   #!/bin/bash
   /opt/safedep/gryph/bin/gryph doctor --managed --json
   ```

### Microsoft Intune (Windows)

1. Package `gryph.exe` and the input files as a Win32 app, or ship them with a
   PowerShell script that runs as `SYSTEM`. Place the binary at
   `%ProgramFiles%\SafeDep\gryph\gryph.exe`. Put the input files below
   `%ProgramData%\SafeDep\gryph-input\` and reset the access control list of
   that folder, because `ProgramData` lets a standard user create files by
   design:

   ```powershell
   $input = "$env:ProgramData\SafeDep\gryph-input"
   New-Item -ItemType Directory -Force $input | Out-Null
   Copy-Item .\managed.yml, .\policy.yaml $input
   icacls $input /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F" "Users:(OI)(CI)RX"
   & "$env:ProgramFiles\SafeDep\gryph\gryph.exe" install --managed `
     --config "$input\managed.yml" --policy "$input\policy.yaml" --json
   exit $LASTEXITCODE
   ```

2. Add a custom compliance policy. The discovery script prints one JSON
   object, and the rule file compares `GryphProfile` with `locked`:

   ```powershell
   $report = & "$env:ProgramFiles\SafeDep\gryph\gryph.exe" doctor --managed --json | ConvertFrom-Json
   @{ GryphProfile = $report.profile } | ConvertTo-Json -Compress
   ```

3. To remove, run `gryph.exe uninstall --managed --json` as `SYSTEM`. On
   Windows the command does not visit the user profiles, because Gryph
   cannot drop to another account there. The report says `users_skipped`.
   Each user runs `gryph uninstall` once, or the agent hook entry stays and
   fails open when the binary is gone.

The reconcile job is a scheduled task for the Users group,
`SafeDep\gryph-reconcile`, that runs at logon and every 15 minutes as the
logged-on user.

### Microsoft Intune (Linux)

Use a custom script that runs as root with the Linux recipe above. The
compliance script is the same `gryph doctor --managed --json` call, with the
exit code as the result.
