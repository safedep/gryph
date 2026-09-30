package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	// failAfter makes each query after the first failAfter queries fail.
	failAfter int
	calls     int
}

func (s *fakeReceiptStore) QueryReceipts(_ context.Context, f *storage.ReceiptFilter) ([]*storage.ReceiptRow, error) {
	s.calls++
	if s.failAfter > 0 && s.calls > s.failAfter {
		return nil, errors.New("database is locked")
	}
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

func newTestFollower(store *fakeReceiptStore, filter storage.ReceiptFilter, out *bytes.Buffer) *receiptFollower {
	return newReceiptFollower(store, filter, out, io.Discard, tui.NewColorizer(false))
}

func pollOnce(t *testing.T, f *receiptFollower) {
	since := f.pollSince()
	rows, err := f.fetch(context.Background(), since)
	require.NoError(t, err)
	require.NoError(t, f.show(rows))
	f.prune(since)
}

func TestReceiptFollower(t *testing.T) {
	ctx := context.Background()

	t.Run("prints the latest rows in time order", func(t *testing.T) {
		now := time.Now()
		store := &fakeReceiptStore{}
		store.add(now.Add(-3*time.Second), "c1")
		store.add(now.Add(-2*time.Second), "c2")
		store.add(now.Add(-1*time.Second), "c3")
		var out bytes.Buffer
		f := newTestFollower(store, storage.ReceiptFilter{}, &out)
		require.NoError(t, f.printInitial(ctx, 2))
		assert.Equal(t, []string{"c2", "c3"}, printedCommands(out.String()))

		out.Reset()
		pollOnce(t, f)
		assert.Empty(t, out.String(), "a row that the limit cut is not printed later, and no row prints twice")
	})

	t.Run("prints new rows once", func(t *testing.T) {
		store := &fakeReceiptStore{}
		var out bytes.Buffer
		f := newTestFollower(store, storage.ReceiptFilter{}, &out)
		require.NoError(t, f.printInitial(ctx, 10))
		assert.Empty(t, out.String())

		at := time.Now()
		store.add(at, "same-a")
		store.add(at, "same-b")
		pollOnce(t, f)
		pollOnce(t, f)
		assert.ElementsMatch(t, []string{"same-a", "same-b"}, printedCommands(out.String()),
			"two rows with one time both print, once")
	})

	t.Run("prints a row that commits late with an older time", func(t *testing.T) {
		store := &fakeReceiptStore{}
		store.add(time.Now().Add(-time.Minute), "old")
		var out bytes.Buffer
		f := newTestFollower(store, storage.ReceiptFilter{}, &out)
		require.NoError(t, f.printInitial(ctx, 10))

		store.add(time.Now(), "new")
		pollOnce(t, f)
		store.add(time.Now().Add(-2*time.Second), "late")
		pollOnce(t, f)
		assert.Equal(t, []string{"old", "new", "late"}, printedCommands(out.String()))
	})

	t.Run("prints a late row older than the first printed row", func(t *testing.T) {
		store := &fakeReceiptStore{}
		store.add(time.Now().Add(-time.Second), "first")
		var out bytes.Buffer
		f := newTestFollower(store, storage.ReceiptFilter{}, &out)
		require.NoError(t, f.printInitial(ctx, 10))

		store.add(time.Now().Add(-3*time.Second), "late")
		pollOnce(t, f)
		assert.Equal(t, []string{"first", "late"}, printedCommands(out.String()))
	})

	t.Run("keeps the since filter", func(t *testing.T) {
		store := &fakeReceiptStore{}
		since := time.Now().Add(-5 * time.Second)
		var out bytes.Buffer
		f := newTestFollower(store, storage.ReceiptFilter{Since: &since}, &out)
		require.NoError(t, f.printInitial(ctx, 10))

		store.add(time.Now().Add(-8*time.Second), "before-since")
		store.add(time.Now(), "after-since")
		pollOnce(t, f)
		assert.Equal(t, []string{"after-since"}, printedCommands(out.String()))
	})
}

func TestReceiptFollower_Follow(t *testing.T) {
	t.Run("stops after repeated poll failures", func(t *testing.T) {
		store := &fakeReceiptStore{failAfter: 2}
		var out, errOut bytes.Buffer
		f := newReceiptFollower(store, storage.ReceiptFilter{}, &out, &errOut, tui.NewColorizer(false))
		tick := make(chan time.Time, receiptFollowMaxFailures)
		for range receiptFollowMaxFailures {
			tick <- time.Now()
		}
		err := f.follow(context.Background(), 10, tick)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database is locked")
		assert.Equal(t, receiptFollowMaxFailures-1, strings.Count(errOut.String(), "retrying"))
	})

	t.Run("returns nil when the context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out bytes.Buffer
		f := newTestFollower(&fakeReceiptStore{}, storage.ReceiptFilter{}, &out)
		assert.NoError(t, f.follow(ctx, 10, make(chan time.Time)))
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
		{"long rule", []string{strings.Repeat("r", 40)}, strings.Repeat("r", 25) + "..."},
		{"long rule with more rules", []string{strings.Repeat("r", 40), "b"}, strings.Repeat("r", 22) + "... +1"},
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
	valid := receiptFollowFlags{format: "table", interval: time.Second, limit: 20}
	tests := []struct {
		name    string
		change  func(*receiptFollowFlags)
		wantErr bool
	}{
		{"valid", func(*receiptFollowFlags) {}, false},
		{"verify", func(f *receiptFollowFlags) { f.verify = true }, true},
		{"until", func(f *receiptFollowFlags) { f.until = time.Hour }, true},
		{"json format", func(f *receiptFollowFlags) { f.format = "json" }, true},
		{"show hash", func(f *receiptFollowFlags) { f.showHash = true }, true},
		{"zero interval", func(f *receiptFollowFlags) { f.interval = 0 }, true},
		{"zero limit", func(f *receiptFollowFlags) { f.limit = 0 }, true},
		{"negative limit", func(f *receiptFollowFlags) { f.limit = -1 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := valid
			tt.change(&flags)
			err := checkReceiptsFollowFlags(flags)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
