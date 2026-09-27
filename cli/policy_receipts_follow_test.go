package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReceiptStore applies Since and Limit, and orders newest first, as the
// SQLite store does without a session filter.
type fakeReceiptStore struct {
	rows []*storage.ReceiptRow
}

func (s *fakeReceiptStore) QueryReceipts(_ context.Context, f *storage.ReceiptFilter) ([]*storage.ReceiptRow, error) {
	var out []*storage.ReceiptRow
	for _, r := range s.rows {
		if f.Since != nil && r.RecordedAt.Before(*f.Since) {
			continue
		}
		out = append(out, r)
	}
	sortReceiptsByTime(out)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *fakeReceiptStore) add(at time.Time, command string) *storage.ReceiptRow {
	r := &storage.ReceiptRow{
		ID:            uuid.New(),
		SessionID:     uuid.New(),
		RecordedAt:    at,
		Agent:         "claude-code",
		Tool:          "Bash",
		Decision:      "block",
		ActionPayload: map[string]interface{}{"command": command},
	}
	s.rows = append(s.rows, r)
	return r
}

func printedCommands(out string) []string {
	var cmds []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		cmds = append(cmds, fields[len(fields)-1])
	}
	return cmds
}

func TestReceiptFollower(t *testing.T) {
	now := time.Now()
	ctx := context.Background()

	t.Run("prints the latest rows in time order", func(t *testing.T) {
		store := &fakeReceiptStore{}
		store.add(now.Add(-3*time.Second), "c1")
		store.add(now.Add(-2*time.Second), "c2")
		store.add(now.Add(-1*time.Second), "c3")
		var out bytes.Buffer
		f := newReceiptFollower(store, storage.ReceiptFilter{}, &out, tui.NewColorizer(false))
		require.NoError(t, f.printInitial(ctx, 2))
		assert.Equal(t, []string{"c2", "c3"}, printedCommands(out.String()))

		out.Reset()
		require.NoError(t, f.poll(ctx))
		assert.Empty(t, out.String(), "a row that the limit cut is not printed later, and no row prints twice")
	})

	t.Run("prints new rows once", func(t *testing.T) {
		store := &fakeReceiptStore{}
		var out bytes.Buffer
		f := newReceiptFollower(store, storage.ReceiptFilter{}, &out, tui.NewColorizer(false))
		require.NoError(t, f.printInitial(ctx, 10))
		assert.Empty(t, out.String())

		at := time.Now()
		store.add(at, "same-a")
		store.add(at, "same-b")
		require.NoError(t, f.poll(ctx))
		require.NoError(t, f.poll(ctx))
		assert.ElementsMatch(t, []string{"same-a", "same-b"}, printedCommands(out.String()),
			"two rows with one time both print, once")
	})

	t.Run("prints a row that commits late with an older time", func(t *testing.T) {
		store := &fakeReceiptStore{}
		store.add(now.Add(-time.Minute), "old")
		var out bytes.Buffer
		f := newReceiptFollower(store, storage.ReceiptFilter{}, &out, tui.NewColorizer(false))
		require.NoError(t, f.printInitial(ctx, 10))

		store.add(time.Now(), "new")
		require.NoError(t, f.poll(ctx))
		store.add(time.Now().Add(-2*time.Second), "late")
		require.NoError(t, f.poll(ctx))
		assert.Equal(t, []string{"old", "new", "late"}, printedCommands(out.String()))
	})
}

func TestFormatReceiptLine_EscapesStoredValues(t *testing.T) {
	r := &storage.ReceiptRow{
		ID:             uuid.New(),
		SessionID:      uuid.New(),
		RecordedAt:     time.Now(),
		Agent:          "a\x1b[2J",
		Tool:           "t\x07",
		Decision:       "block",
		MatchedRuleIDs: []string{"rule\x1b]0;x\x07"},
		ActionPayload:  map[string]interface{}{"command": "echo hi\nforged row"},
	}
	line := formatReceiptLine(tui.NewColorizer(false), r)
	assert.NotContains(t, line, "\x1b")
	assert.NotContains(t, line, "\x07")
	assert.NotContains(t, line, "\n")
}

func TestReceiptRuleCell(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{"no rule", nil, "-"},
		{"one rule", []string{"block-npm"}, "block-npm"},
		{"more rules", []string{"block-npm", "warn-net", "tag-web"}, "block-npm +2"},
		{"long rule", []string{strings.Repeat("r", 40)}, strings.Repeat("r", 21) + "..."},
		{"control character", []string{"a\x1bb"}, "a\uFFFDb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, receiptRuleCell(tt.ids))
		})
	}
}

func TestReceiptTarget(t *testing.T) {
	assert.Equal(t, "npm install", receiptTarget(map[string]interface{}{"command": "npm install", "path": "/p"}))
	assert.Equal(t, "/p/.env", receiptTarget(map[string]interface{}{"path": "/p/.env"}))
	assert.Equal(t, "https://x.test", receiptTarget(map[string]interface{}{"url": "https://x.test"}))
	assert.Equal(t, "", receiptTarget(nil))
}

func TestCheckReceiptsFollowFlags(t *testing.T) {
	assert.NoError(t, checkReceiptsFollowFlags(false, 0, "table", false, time.Second))
	assert.Error(t, checkReceiptsFollowFlags(true, 0, "table", false, time.Second))
	assert.Error(t, checkReceiptsFollowFlags(false, time.Hour, "table", false, time.Second))
	assert.Error(t, checkReceiptsFollowFlags(false, 0, "json", false, time.Second))
	assert.Error(t, checkReceiptsFollowFlags(false, 0, "table", true, time.Second))
	assert.Error(t, checkReceiptsFollowFlags(false, 0, "table", false, 0))
}
