# Content Labels

Most agent content that Gryph stores is a `privacy.Text`: the value and a
`privacy.Label`. Exporters and policy read the label. They do not need the
current config to know what the value is.

Some fields have no label. `Event.ErrorMessage` stays a string, and
`Event.RawEvent` stays raw JSON. The export section lists the plain string
payload fields and how the export treats them.

## Types

`core/privacy` holds the types. It imports no Gryph package.

| Type | Meaning |
|---|---|
| `Text` | `Value` and `Label`. JSON: `{"value": "...", "label": {...}}` |
| `Label` | `Classes`, `Origin`, `Source`, `Redacted`, `Truncated`, `Stripped`, `Level`, `Size`, `Digest`, `Unclassified` |
| `Class` | A closed set: `secret`, `pii`, `source_code`, `config`, `git_internal`, `external_url`, `unknown_sensitive` |
| `Origin` | A closed set: `user`, `agent`, `file_project`, `file_external`, `command`, `web`, `mcp`, `unknown` |
| `Redactor` | The sensitive-path globs and the redaction regexps |
| `Walk` | Visits every `Text` in a value by reflection |

A class is a fact from the built-in heuristic classifier (`aarm/classify`).
It is not a config value. `policy.classify.extra_patterns` can add globs to
a class. A key that is not a class, such as `customer_data`, is a custom
policy label. CEL reads it in `action.data_classifications` and
`context.classifications_seen`, so a rule on it still matches. It never
becomes a content label, and export never sends it. `ClassifyEvent` drops
it. The admin's own names for events are rule tags, not classes.

## Fields

These payload fields are `privacy.Text`:

- `FileWritePayload`: `ContentPreview`, `OldString`, `NewString`
- `CommandExecPayload`: `Command`, `Output`, `StdoutPreview`, `StderrPreview`
- `ToolUsePayload`: `Input`, `Output`, `OutputPreview`. `Input` and `Output`
  hold the tool JSON as a string. Parse `Value` to read the structure.
- `SubagentStopPayload`: `LastAssistantMessage`
- `UserPromptPayload`: `Prompt`. `Event.SetPrompt` sets the origin: `user`
  for a prompt that a person typed, `agent` for a prompt that agent code
  injected.
- `Event`: `DiffContent`. The `diff_label` column stores its label.

Identifiers stay `string`: paths, URLs, tool names, and session IDs.
`TestPayloadStringFields` in `core/events` lists the allowed `string` fields.
A new content field that is a `string` fails the test.

`Text` reads the old row forms. A bare JSON string becomes `Value`. Any other
JSON value, such as an old tool input object, becomes its compact JSON text.
So rows from before labels stay readable with no data migration. The storage
filters on the command read `$.command.value` and fall back to `$.command`.

An old form that is not empty gets the label `{"unclassified": true}`. Gryph
did not classify or redact such a value, so an empty class list on it does
not mean "holds no secret". Storage also sets `Unclassified` on a diff that
has no `diff_label`, because the label step gives every diff a digest. Only
a read sets the flag. The label step clears it when an old event goes
through the label step again. A new row never has the key.

## The label step

`decision.Local` labels an event in two steps, around the policy
evaluation. `labelEvent` (`decision/label.go`) runs before the evaluation:

1. Set `Digest` (`sha256:<hex>`) and `Size` from the raw value. An adapter
   that truncates a preview uses `privacy.Preview`, which sets them from the
   whole value and sets `Truncated`.
2. Set `Classes` from the classifier (`classify.Heuristic.ClassifyEvent`) and
   the sensitive-path check. The classifier reads the paths and the URL that
   `Event.Targets` returns.
3. Set `Origin` and `Source` when the value has no origin yet. The tool
   output (`output`, `stdout_preview`, `stderr_preview`, `output_preview`)
   takes the event origin, and the MCP server for an `mcp` origin. Every
   other value, such as the command, the tool input, or the content of a
   write, takes `agent`. A prompt keeps the origin that `SetPrompt` gave it.
4. Apply the redactor. Set `Redacted` when a pattern matched. A value that
   holds a JSON object or array keeps its structure. The redactor also
   applies to the raw event and to the error message.

A payload that does not decode into the payload type of its action goes
to the policy as it is. The policy cannot read it, so its fail mode decides,
and `fail_mode: closed` blocks the action. Gryph cannot label, redact or
strip such a payload, so it drops the payload after the evaluation, with a
warning in the log, and does not store it at any level.

The policy then evaluates the redacted event. It sees the content at every
logging level, so a rule on a URL or on content still fires at `minimal`.
The AARM receipt records the action with the rule of `decision.StripsContent`.
When the stored event loses its content, the receipt drops the URL, the line
counts, and every parameter that a tool-use action takes from the tool
input. So a receipt never keeps what the stored event loses.

A rule message can name a content value. The PDP renders the message for
the agent from the full action. It renders the stored message from the
action that the receipt records. The receipt and the `error_message` of a
blocked event keep only the stored message. `decision.Local` runs the
redactor on it before the save.

`applyLevel` runs after the evaluation and before the save:

5. Apply the logging level. Set `Stripped` when the level removes the value.
   Set `Level` to the logging level in force.

Only the `secret` class makes an event sensitive. The sensitive-path check
adds it. The other classes, `pii` included, are facts on the label. The
heuristic `pii` globs are coarse, and a sensitive event loses its content.

## Logging levels

| Level | Stored content |
|---|---|
| `full` | Every value, and the diff, the raw event, and the conversation context |
| `standard` | The payload values, except the prompt. The diff, the prompt, the raw event, and the context are removed |
| `minimal` | Labels only, except the command of a `command_exec` event |

A prompt holds what the user typed, which can be a pasted secret or private
text. So only `full` keeps it. The policy still reads the whole prompt,
because the level applies after the evaluation.

A sensitive event keeps labels only, at every level, except the command. An
event is sensitive when its path matches `privacy.sensitive_paths`, or when
its labels have class `secret`. Content labels use the heuristic without the
fail-safe wrapper. The wrapper adds `unknown_sensitive` to every
unclassified action.

## Export

An export profile decides what happens to each content value that leaves
the machine. Each value gets one treatment:

| Treatment | Value        | Label                       |
|-----------|--------------|-----------------------------|
| `include` | kept         | kept, without the digest    |
| `redact`  | `[REDACTED]` | kept, with the keyed digest |
| `digest`  | emptied      | kept, with the keyed digest |
| `drop`    | removed      | removed, except the size    |

An included value carries no digest, because the export holds the value. A
reader cannot match a digest to an included value with the same text.

A digest never leaves the machine as a plain sha256. A dictionary recovers
a short prompt, such as `yes`, or a short password from its plain sha256.
The export holds `hmac-sha256:<hex>`, an HMAC-SHA256 of this input:

```
<profile name> 0x00 <origin> 0x00 sha256:<hex>
```

The origin is the origin of the label, or an empty string when the label
has none. The `content_hash` of a file payload uses `content_hash` in
place of the origin. So one install matches its own values only within one
profile and one origin. The prompt `make deploy` under `default` and the
same text under `metadata` give different digests. The same text with
origin `user` and with origin `agent` also give different digests.

The key is 32 random bytes in `export.key`, next to the database. Gryph
creates it on the first export and never exports it. Only the current user
can read the new file: mode 0600 on Unix, and on Windows a protected DACL
for the current user, SYSTEM and Administrators. Another install cannot
match the digests. When you delete the key, the new key gives new digests.

Gryph refuses to read a key file that another user owns or that grants
access to another user. On Unix, that is a mode with group or other bits,
another owner uid, or a symbolic link. On Windows, that is an owner or a
DACL entry that allows read, write, delete or a permission change for a
principal other than the current user, SYSTEM or Administrators. A key in
the default data directory passes. A key in a directory that
`GRYPH_DATA_DIR` names can inherit a wider DACL. The error tells you the
`chmod`, `chown` or `icacls` command that fixes the file. The same checks
apply to the receipt private key. The self-protection rules block an agent
read, write or removal of `export.key`. An agent that reads the key can reverse a digest
with a dictionary. An agent that writes a known key can do the same with
later exports.

Every profile removes the digest and the size of a value that:

- Gryph redacted, truncated, or stripped. The digest covers content that
  the export does not show, and that content can hold a secret that the
  redactor did not see, such as a secret after the truncation cut.
- has the class `secret`, `pii` or `unknown_sensitive`. A digest of a short
  secret or a phone number can be reversed by brute force.

The local store keeps the plain digest, so `gryph cat --format json` shows
it.

Gryph has four built-in profiles:

- `default` drops content with class `secret`, `pii` or `unknown_sensitive`,
  digests content with origin `user` (prompts), and includes the rest.
- `metadata` digests every value.
- `full` includes every value.
- `policy` keeps the fields that rules match on and sends no content. It
  drops content with class `secret`, `pii` or `unknown_sensitive`, digests
  prompts and every content field (`output`, `stdout_preview`,
  `stderr_preview`, `input`, `output_preview`, `content_preview`,
  `old_string`, `new_string`, `last_assistant_message`, `diff_content`),
  and includes the command and the paths. On every value it includes, it
  removes the userinfo, the query and the fragment of each URL, and runs
  the redactor once more with the patterns in force at export time, so a
  pattern added after the row was stored applies. The raw event never
  leaves under it. The [collection level](./supervisor.md#the-collection-level)
  of a managed host names it.

Add a profile under `export.profiles`. A rule matches a value when its label
has any of the classes or any of the origins, or when the value sits in any
of the fields. The first matching rule wins. A value that no rule matches
gets `default`. A profile cannot use a built-in name. A field is the JSON
name of a content field: `command`, `output`, `stdout_preview`,
`stderr_preview`, `input`, `output_preview`, `content_preview`,
`old_string`, `new_string`, `last_assistant_message`, `prompt` or
`diff_content`. A rule that names a field no event has is an error, so a
typo cannot leave a field with a weaker treatment. A profile can also set
`strip_urls: true` and `redact_again: true`, which do what the `policy`
profile does on the values it includes.

An invalid profile, such as one with an unknown class, does not make the
config fail. The hooks keep the rest of the config. Gryph logs a warning,
and `gryph export` and stream sync fail for that profile. A stream target
that names an invalid profile gets no events, and never falls back to a
weaker profile. `gryph config set` rejects a change while a profile is
invalid.

```yaml
export:
  profiles:
    team:
      default: digest
      strip_urls: true
      rules:
        - classes: [secret, pii]
          then: drop
        - origins: [user]
          then: redact
        - fields: [command]
          then: include
```

`Event.ForExport(profile)` makes the copy that leaves the machine. `gryph
export --export-profile` and stream sync use it.

- It decodes and encodes the payload again, so an old row has the same
  shape as a new row.
- It keeps the raw event only when the profile includes every value,
  because the raw event holds every value without a label.
- It drops a payload with no typed struct, or a payload that does not
  decode, unless the profile includes every value.
- A plain string field has no label of its own. These fields are
  `error_message`, the command `description` and `args`, the read
  `pattern`, the session end `reason`, and the notification `message` and
  `details`. Each gets the most restrictive treatment of any value in the
  event. A path has no label either, and a rule matches on it, so the
  export keeps it. A profile with `redact_again` or `strip_urls` applies
  both to every plain value and path it includes.
- The `content_hash` of a file read or write is a plain sha256 of the
  whole file content. The export keys it as a digest, with the scope
  `content_hash` in place of the origin. It removes the hash
  when the plain field treatment is not `include`, when the event is
  sensitive, or when any value in the event loses its digest by the rules
  above.
- A row from before content labels has no class and no origin. A sensitive row gets the
  class `secret` on every value, and a prompt gets the origin `user`. The
  export keeps `unclassified: true` on each old value, also when the profile
  drops the value. The export does not run the classifier or the redactor
  on old values, because that changes what old data claims about itself.

Stream sync also removes `details.raw_event` and the error of a hook error
self-audit, unless the profile includes every value.

`gryph cat` shows each content value as its text, and a stripped value as
`[stripped]`. It adds `[unclassified]` after an unclassified value and after
the `Diff` header of an unclassified diff. The CSV format has a `diff_unclassified` column.

## Adding a content field

1. Declare it as `privacy.Text` with `json:",omitzero"`.
2. Nothing else. `Walk` finds it, so the label step and the export
   profiles treat it with no new code.
