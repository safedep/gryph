package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/platform/nofollow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSink struct {
	entries []Entry
	uids    []uint32
	reports map[uint32]Report
	fail    bool
}

func (f *fakeSink) Entry(_ context.Context, uid uint32, entry Entry) error {
	if f.fail {
		return errors.New("store closed")
	}
	f.entries = append(f.entries, entry)
	f.uids = append(f.uids, uid)
	return nil
}

func (f *fakeSink) Report(_ context.Context, uid uint32, report Report) {
	if f.reports == nil {
		f.reports = map[uint32]Report{}
	}
	f.reports[uid] = report
}

func handleFrame() *ipc.Frame {
	return ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(`{}`)})
}

func openRoot(t *testing.T, root string) *nofollow.Dir {
	t.Helper()
	d, err := nofollow.OpenDir(root, ".")
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestIngest_TakesEntriesOfTheOwner(t *testing.T) {
	root := t.TempDir()
	me := strconv.Itoa(os.Getuid())
	first, err := Write(root, me, Entry{Verdict: "allow", Reason: "unreachable", Frame: handleFrame()})
	require.NoError(t, err)
	_, err = Write(root, me, Entry{Kind: KindTamper, Reason: "bad socket"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, me, ".partial.tmp"), []byte("{"), 0o600))

	sink := &fakeSink{}
	require.NoError(t, Ingest(context.Background(), openRoot(t, root), DefaultLimits(), sink))
	require.Len(t, sink.entries, 2)
	assert.Equal(t, "unreachable", sink.entries[0].Reason)
	assert.Equal(t, KindTamper, sink.entries[1].Kind)
	assert.Equal(t, uint32(os.Getuid()), sink.uids[0])
	assert.Empty(t, sink.reports)
	assert.Equal(t, []string{".partial.tmp"}, names(t, filepath.Dir(first)), "taken entries are removed, a partial write stays")
}

func TestIngest_RefusesWhatIsNotAnEntry(t *testing.T) {
	root := t.TempDir()
	me := strconv.Itoa(os.Getuid())
	dir := filepath.Join(root, me)
	require.NoError(t, os.Mkdir(dir, 0o700))
	elsewhere := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(elsewhere, []byte(`{"reason":"planted"}`), 0o600))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(dir, "1-link.json")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2-two-links.json"), []byte(`{"reason":"x"}`), 0o600))
	require.NoError(t, os.Link(filepath.Join(dir, "2-two-links.json"), filepath.Join(t.TempDir(), "other-name")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "3-not-json.json"), []byte(`nope`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "4-big.json"), []byte(strings.Repeat("x", 200)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "5-bad-frame.json"), []byte(`{"reason":"r","frame":{"type":"handle","body":{"agent":"","hook_type":"x","raw_payload":""}}}`), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "6-dir"), 0o700))
	_, err := Write(root, me, Entry{Verdict: "allow", Reason: "good"})
	require.NoError(t, err)

	limits := DefaultLimits()
	limits.MaxFileBytes = 150
	sink := &fakeSink{}
	require.NoError(t, Ingest(context.Background(), openRoot(t, root), limits, sink))
	require.Len(t, sink.entries, 1)
	assert.Equal(t, "good", sink.entries[0].Reason)

	report := sink.reports[uint32(os.Getuid())]
	var refused []string
	for _, r := range report.Refused {
		refused = append(refused, r.Name+": "+r.Reason)
	}
	require.Len(t, refused, 6, strings.Join(refused, "\n"))
	assert.Contains(t, refused[0], "1-link.json: ")
	assert.Contains(t, refused[0], "symbolic link")
	assert.Equal(t, "2-two-links.json: 2 links", refused[1])
	assert.Contains(t, refused[2], "3-not-json.json: not an entry")
	assert.Equal(t, "4-big.json: 200 bytes, over the 150 byte cap", refused[3])
	assert.Contains(t, refused[4], "5-bad-frame.json: frame refused")
	assert.Contains(t, refused[5], "6-dir: ")
	assert.Equal(t, 0, report.Dropped)

	data, err := os.ReadFile(elsewhere)
	require.NoError(t, err)
	assert.Equal(t, `{"reason":"planted"}`, string(data), "the target of the link is never read or changed")
	assert.Equal(t, []string{"6-dir"}, names(t, dir), "refused files are removed, a directory stays")
}

func TestIngest_DropsOverTheQuota(t *testing.T) {
	root := t.TempDir()
	me := strconv.Itoa(os.Getuid())
	for i := range 5 {
		_, err := Write(root, me, Entry{Verdict: "allow", Reason: strconv.Itoa(i)})
		require.NoError(t, err)
	}
	limits := DefaultLimits()
	limits.MaxFiles = 3
	sink := &fakeSink{}
	require.NoError(t, Ingest(context.Background(), openRoot(t, root), limits, sink))
	require.Len(t, sink.entries, 3)
	assert.Equal(t, "0", sink.entries[0].Reason, "the oldest entries are taken")
	assert.Equal(t, 2, sink.reports[uint32(os.Getuid())].Dropped)
	assert.Empty(t, names(t, filepath.Join(root, me)))
}

func TestIngest_KeepsTheFileWhenTheSinkFails(t *testing.T) {
	root := t.TempDir()
	me := strconv.Itoa(os.Getuid())
	path, err := Write(root, me, Entry{Verdict: "allow", Reason: "r"})
	require.NoError(t, err)
	sink := &fakeSink{fail: true}
	err = Ingest(context.Background(), openRoot(t, root), DefaultLimits(), sink)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store closed")
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestIngest_SkipsFilesAtTheRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.json"), []byte(`{}`), 0o600))
	sink := &fakeSink{}
	require.NoError(t, Ingest(context.Background(), openRoot(t, root), DefaultLimits(), sink))
	assert.Empty(t, sink.entries)
	_, err := os.Stat(filepath.Join(root, "stray.json"))
	assert.NoError(t, err)
}

func TestEnsureRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "spool")
	require.NoError(t, EnsureRoot(root))
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, RootMode, info.Mode()&(os.ModePerm|os.ModeSetgid|os.ModeSticky))
	require.NoError(t, os.Chmod(root, 0o700))
	require.NoError(t, EnsureRoot(root), "an existing root keeps its mode")
	info, err = os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	assert.Error(t, EnsureRoot(""))
}
