# Streams

Streams export stored events and self-audit entries to external destinations called **targets**. A built-in syncer handles batching and checkpointing so each target only receives new data.

## Quick start

The default config ships with an enabled `stdout` target. Run:

```bash
gryph stream sync
```

This prints all unsynced events and audit entries as JSON to stdout.

## Configuration

Targets live under `streams.targets` in `~/.config/gryph/config.yaml` (Linux) or `~/Library/Application Support/gryph/config.yaml` (macOS):

```yaml
streams:
  targets:
    - name: stdout
      type: stdout
      enabled: true
    - name: my-webhook
      type: webhook        # hypothetical custom target
      enabled: false
      config:
        url: https://example.com/ingest
```

Each target has:

| Field     | Required | Description                              |
|-----------|----------|------------------------------------------|
| `name`    | yes      | Unique identifier (used for checkpoints) |
| `type`    | yes      | Target type (must be registered)         |
| `enabled` | yes      | Whether the syncer sends to this target  |
| `config`  | no       | Arbitrary key-value map for the target   |

Two targets can share a type but **must** have different names. Checkpoints are tracked per name, so renaming a target resets its sync position.

## Architecture

```
core/stream/target.go   Target interface + StreamItem type
stream/registry.go      Thread-safe target registry + type constants
stream/sync.go          Syncer (batch query, send, checkpoint)
stream/stdout/           Reference target implementation
config/                  Validation + defaults
cli/stream.go           CLI wiring (registry build + sync command)
storage/                 Checkpoint persistence (ent/SQLite)
```

### Sync flow

1. `gryph stream sync` builds a registry from config and creates a `Syncer`.
2. For each enabled target the syncer:
   - Loads checkpoint (`last_synced_at` timestamp)
   - Queries events and self-audits newer than that timestamp (batch size: 500)
   - Packs them into `[]StreamItem` and calls `target.Send()`
   - Saves a new checkpoint with the latest timestamp and IDs

Checkpoints are keyed by `target.Name()`, so multiple instances of the same type with different names track progress independently.

## Writing a new target

### 1. Implement the interface

Create a package under `stream/`, e.g. `stream/webhook/`:

```go
package webhook

import (
    "context"
    "fmt"
    "net/http"
    "encoding/json"
    "bytes"

    corestream "github.com/safedep/gryph/core/stream"
    "github.com/safedep/gryph/stream"
)

type Target struct {
    name    string
    enabled bool
    url     string
}

func New(name string, enabled bool, cfg map[string]any) *Target {
    url, _ := cfg["url"].(string)
    return &Target{name: name, enabled: enabled, url: url}
}

func (t *Target) Name() string  { return t.name }
func (t *Target) Type() string  { return stream.TargetTypeWebhook }
func (t *Target) Enabled() bool { return t.enabled }

func (t *Target) Send(ctx context.Context, items []corestream.StreamItem) error {
    body, err := json.Marshal(items)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("send: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return fmt.Errorf("target returned %d", resp.StatusCode)
    }
    return nil
}

func (t *Target) Close() error { return nil }

var _ corestream.Target = (*Target)(nil)
```

### 2. Register the type constant

In `stream/registry.go`:

```go
const (
    TargetTypeStdout  = "stdout"
    TargetTypeWebhook = "webhook"
)
```

### 3. Allow it in config validation

In `config/config.go` add the constant:

```go
const (
    streamTargetTypeStdout  = "stdout"
    streamTargetTypeWebhook = "webhook"
)
```

In `config/validate.go` add to the map:

```go
var knownStreamTargetTypes = map[string]bool{
    streamTargetTypeStdout:  true,
    streamTargetTypeWebhook: true,
}
```

### 4. Wire it in the CLI factory

In `cli/stream.go` `buildStreamRegistry`:

```go
case stream.TargetTypeWebhook:
    registry.Register(webhook.New(tc.Name, tc.Enabled, tc.Config))
```

### 5. Build and test

```bash
make gryph && make test
```

## StreamItem shape

Each item sent to a target contains either an event, a self-audit entry, or both:

```go
type StreamItem struct {
    Event     *events.Event           `json:"event,omitempty"`
    SelfAudit *storage.SelfAuditEntry `json:"self_audit,omitempty"`
}
```

Targets should handle either field being nil.

## Tips

- `Send()` receives a batch. If your destination has its own batching, you can split or buffer internally.
- Return an error from `Send()` to abort the sync for that target. The checkpoint won't advance, so the next run retries the same batch.
- Use `Close()` for connection cleanup (flush buffers, close sockets, etc.).
- The `Config` map (`map[string]any`) passes through arbitrary YAML from the target config block. Type-assert what you need in your constructor.
