# Content Labels

Every piece of agent content that Gryph stores is a `privacy.Text`: the value
and a `privacy.Label`. Exporters and policy read the label. They do not need
the current config to know what the value is.

## Types

`core/privacy` holds the types. It imports no Gryph package.

| Type | Meaning |
|---|---|
| `Text` | `Value` and `Label`. JSON: `{"value": "...", "label": {...}}` |
| `Label` | `Classes`, `Origin`, `Source`, `Redacted`, `Truncated`, `Stripped`, `Level`, `Size`, `Digest` |
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
- `UserPromptPayload`: `Prompt`. `Event.SetPrompt` sets the origin `user`.
- `Event`: `DiffContent`. The `diff_label` column stores its label.

Identifiers stay `string`: paths, URLs, tool names, and session IDs.
`TestPayloadStringFields` in `core/events` lists the allowed `string` fields.
A new content field that is a `string` fails the test.

`Text` reads the old row forms. A bare JSON string becomes `Value` with an
empty label. Any other JSON value, such as an old tool input object, becomes
its compact JSON text. So rows from before labels stay readable with no data
migration. The storage filters on the command read `$.command.value` and fall
back to `$.command`.

## The label step

`decision.Local` labels an event in two steps, around the policy
evaluation. `labelEvent` (`decision/label.go`) runs before the evaluation:

1. Set `Digest` (`sha256:<hex>`) and `Size` from the raw value. An adapter
   that truncates a preview uses `privacy.Preview`, which sets them from the
   whole value and sets `Truncated`.
2. Set `Classes` from the classifier (`classify.Heuristic.ClassifyEvent`) and
   the sensitive-path check. The classifier reads the paths and the URL that
   `Event.Targets` returns.
3. `Origin` and `Source` are claims from the adapter. No adapter sets them
   yet.
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

`Event.ForExport` makes the copy that leaves the machine. `gryph export` and
stream sync use it. It decodes and encodes the payload again, so an old row
has the same shape as a new row.

Export keeps the digest and the size only when the exported value is the
whole original value. It removes them when the label has `Redacted`,
`Truncated`, or `Stripped`, or has the `secret` class. The digest and the
size describe the whole original value. For a truncated or stripped value,
that original can hold a secret that the export does not show. A digest of
a short secret can be reversed by brute force. The local store keeps the
digest, so `gryph cat --format json` shows it. Export profiles replace
`Text.ForExport`. Every export profile must keep this rule.

`gryph cat` shows each content value as its text, and a stripped value as
`[stripped]`.

## Adding a content field

1. Declare it as `privacy.Text` with `json:",omitzero"`.
2. Nothing else. `Walk` finds it, so the label step and later export
   profiles treat it with no new code.
