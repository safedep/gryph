package stream

import (
	"context"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	corestream "github.com/safedep/gryph/core/stream"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureTarget struct {
	name  string
	items []corestream.StreamItem
}

func (t *captureTarget) Name() string  { return t.name }
func (t *captureTarget) Type() string  { return TargetTypeNop }
func (t *captureTarget) Enabled() bool { return true }
func (t *captureTarget) Close() error  { return nil }
func (t *captureTarget) Send(_ context.Context, items []corestream.StreamItem) error {
	t.items = append(t.items, items...)
	return nil
}

func TestSyncer_AppliesTargetProfile(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	sess := session.NewSession("claude-code")
	require.NoError(t, store.SaveSession(ctx, sess))

	event := events.NewEvent(sess.ID, "claude-code", events.ActionUserPrompt)
	prompt := privacy.NewText("rename the loader")
	prompt.Label.Origin = privacy.OriginUser
	prompt.Label.Digest = privacy.Digest(prompt.Value)
	require.NoError(t, event.SetPayload(events.UserPromptPayload{Prompt: prompt}))
	event.RawEvent = []byte(`{"prompt":"rename the loader"}`)
	require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))

	full := &captureTarget{name: "full"}
	plain := &captureTarget{name: "plain"}
	registry := NewRegistry()
	registry.Register(full)
	registry.Register(plain)
	syncer := NewSyncer(store, registry)
	syncer.SetProfile(full.name, privacy.BuiltinProfiles()[privacy.ProfileFull])

	_, err := syncer.Sync(ctx)
	require.NoError(t, err)

	promptOf := func(items []corestream.StreamItem) (privacy.Text, []byte) {
		require.Len(t, items, 1)
		payload, err := items[0].Event.DecodePayload()
		require.NoError(t, err)
		p, ok := payload.(*events.UserPromptPayload)
		require.True(t, ok)
		return p.Prompt, items[0].Event.RawEvent
	}

	got, raw := promptOf(full.items)
	assert.Equal(t, "rename the loader", got.Value)
	assert.NotEmpty(t, raw)

	got, raw = promptOf(plain.items)
	assert.Empty(t, got.Value, "a target without a profile gets the default profile")
	assert.Equal(t, prompt.Label.Digest, got.Label.Digest)
	assert.Empty(t, raw, "the raw event holds the prompt unlabeled")
}

func TestSelfAuditForExport(t *testing.T) {
	hookError := &storage.SelfAuditEntry{
		Action:       "hook_error",
		Details:      map[string]interface{}{"hook_type": "UserPromptSubmit", "raw_event": `{"prompt":"secret plan"}`},
		ErrorMessage: `parse {"prompt":"secret plan"}`,
	}
	profiles := privacy.BuiltinProfiles()

	out := selfAuditForExport(hookError, profiles[privacy.ProfileDefault])
	assert.NotContains(t, out.Details, "raw_event")
	assert.Equal(t, "UserPromptSubmit", out.Details["hook_type"])
	assert.Empty(t, out.ErrorMessage)
	assert.Contains(t, hookError.Details, "raw_event", "the source entry does not change")

	out = selfAuditForExport(hookError, profiles[privacy.ProfileFull])
	assert.Contains(t, out.Details, "raw_event")

	install := &storage.SelfAuditEntry{Action: "install", ErrorMessage: "permission denied"}
	assert.Equal(t, "permission denied", selfAuditForExport(install, profiles[privacy.ProfileMetadata]).ErrorMessage)
}
