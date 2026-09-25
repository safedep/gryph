package accumulator

import (
	"cmp"
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordWrite stores a write event whose preview label has the level, and
// appends its context entry.
func recordWrite(t *testing.T, acc *SQLiteAccumulator, store *storage.SQLiteStore, sessionID uuid.UUID, text, level string) {
	t.Helper()
	ctx := context.Background()
	event := events.NewEvent(sessionID, "claude-code", events.ActionFileWrite)
	preview := privacy.NewText(text)
	preview.Label.Level = level
	preview.Label.Digest = privacy.Digest(text)
	require.NoError(t, event.SetPayload(events.FileWritePayload{Path: "/work/a.txt", ContentPreview: preview}))
	require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
	entry := newEntry(sessionID, events.KindAction, model.ActionFileWrite, "Write")
	entry.EventID = event.ID
	require.NoError(t, acc.Append(ctx, entry))
}

func TestSQLiteAccumulator_Window(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	saveSession(t, store, sess)

	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindIntent, model.ActionUserPrompt, "")))
	recordWrite(t, acc, store, sess.ID, "stored at full", "full")
	recordWrite(t, acc, store, sess.ID, "stored at standard", "standard")
	for range 3 {
		require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))
	}

	t.Run("keeps the latest intent", func(t *testing.T) {
		w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 2})
		require.NoError(t, err)
		require.Len(t, w.Entries, 3)
		assert.Equal(t, events.KindIntent, w.Entries[0].Entry.Kind)
		assert.Equal(t, int64(1), w.Entries[0].Entry.Sequence)
		assert.Equal(t, []int64{1, 5, 6}, []int64{w.Entries[0].Entry.Sequence, w.Entries[1].Entry.Sequence, w.Entries[2].Entry.Sequence})
		assert.Nil(t, w.Entries[1].Content, "no content without IncludeContent")
	})

	t.Run("content by level", func(t *testing.T) {
		w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 10, IncludeContent: true})
		require.NoError(t, err)
		require.Len(t, w.Entries, 6)
		require.Len(t, w.Entries[1].Content, 1)
		assert.Equal(t, "stored at full", w.Entries[1].Content[0].Value)
		require.Len(t, w.Entries[2].Content, 1)
		assert.Empty(t, w.Entries[2].Content[0].Value, "a value stored at standard keeps its label only")
		assert.Equal(t, privacy.Digest("stored at standard"), w.Entries[2].Content[0].Label.Digest)
		assert.False(t, w.Truncated)
	})

	t.Run("max bytes drops the oldest content first", func(t *testing.T) {
		recordWrite(t, acc, store, sess.ID, "newest at full", "full")
		w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 10, MaxBytes: len("newest at full"), IncludeContent: true})
		require.NoError(t, err)
		assert.True(t, w.Truncated)
		assert.Empty(t, w.Entries[1].Content[0].Value)
		last := w.Entries[len(w.Entries)-1]
		assert.Equal(t, "newest at full", last.Content[0].Value)
	})

	t.Run("kinds", func(t *testing.T) {
		w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 10, Kinds: []model.EntryKind{events.KindAction}})
		require.NoError(t, err)
		for _, e := range w.Entries {
			assert.Equal(t, events.KindAction, e.Entry.Kind)
		}
	})

	t.Run("canonical bytes are stable", func(t *testing.T) {
		spec := model.WindowSpec{MaxEntries: 10, IncludeContent: true}
		w1, err := acc.Window(ctx, sess.ID, spec)
		require.NoError(t, err)
		w2, err := acc.Window(ctx, sess.ID, spec)
		require.NoError(t, err)
		b1, err := CanonicalWindow(w1)
		require.NoError(t, err)
		b2, err := CanonicalWindow(w2)
		require.NoError(t, err)
		assert.Equal(t, b1, b2)
		assert.Contains(t, string(b1), `"session_id":"`+sess.ID.String()+`"`)
	})
}

func TestSQLiteAccumulator_WindowBlockedPrompt(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	saveSession(t, store, sess)

	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindIntent, model.ActionUserPrompt, "")))
	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))
	blocked := newEntry(sess.ID, events.KindIntent, model.ActionUserPrompt, "")
	blocked.Decision = model.DecisionBlock
	require.NoError(t, acc.Append(ctx, blocked))
	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))

	w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 1})
	require.NoError(t, err)
	require.Len(t, w.Entries, 2)
	assert.Equal(t, int64(1), w.Entries[0].Entry.Sequence, "a blocked prompt never reached the agent")
	assert.Equal(t, int64(4), w.Entries[1].Entry.Sequence)
}

func TestSQLiteAccumulator_WindowWithoutEntries(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	w, err := acc.Window(context.Background(), uuid.New(), model.WindowSpec{MaxEntries: 10, IncludeContent: true})
	require.NoError(t, err)
	assert.Empty(t, w.Entries)
	assert.False(t, w.Truncated)
}

func TestSQLiteAccumulator_WindowLegacyRows(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	saveSession(t, store, sess)

	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))
	missing := newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")
	missing.EventID = uuid.New()
	require.NoError(t, acc.Append(ctx, missing))

	w, err := acc.Window(ctx, sess.ID, model.WindowSpec{MaxEntries: 10, IncludeContent: true})
	require.NoError(t, err)
	require.Len(t, w.Entries, 2)
	for _, e := range w.Entries {
		assert.Empty(t, e.Content)
	}
}

func TestFitBytes(t *testing.T) {
	text := func(v string) privacy.Text { return privacy.Text{Value: v} }
	w := &model.Window{Entries: []model.WindowEntry{
		{Content: []privacy.Text{text("aaaa"), text("bb")}},
		{Content: []privacy.Text{text("cccc")}},
	}}

	assert.True(t, fitBytes(w, 6))
	assert.Empty(t, w.Entries[0].Content[0].Value)
	assert.Equal(t, "bb", w.Entries[0].Content[1].Value, "fitBytes stops once the total fits")
	assert.Equal(t, "cccc", w.Entries[1].Content[0].Value)
	assert.False(t, fitBytes(w, 0))
}

func TestSQLiteAccumulator_WindowOrdersBySequence(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	saveSession(t, store, sess)

	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindIntent, model.ActionUserPrompt, "")))
	for range 3 {
		require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))
	}

	for _, spec := range []model.WindowSpec{
		{MaxEntries: 2},
		{MaxEntries: 2, Kinds: []model.EntryKind{events.KindAction, events.KindIntent}},
		{MaxEntries: 10},
	} {
		w, err := acc.Window(ctx, sess.ID, spec)
		require.NoError(t, err)
		assert.True(t, slices.IsSortedFunc(w.Entries, func(a, b model.WindowEntry) int {
			return cmp.Compare(a.Entry.Sequence, b.Entry.Sequence)
		}), "spec %+v", spec)
		assert.Equal(t, int64(1), w.Entries[0].Entry.Sequence)
	}
}
