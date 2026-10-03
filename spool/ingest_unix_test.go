//go:build !windows

package spool

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestIngest_DoesNotWaitOnAFIFO(t *testing.T) {
	root := t.TempDir()
	me := strconv.Itoa(os.Getuid())
	dir := filepath.Join(root, me)
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, unix.Mkfifo(filepath.Join(dir, "1-pipe.json"), 0o600))
	_, err := Write(root, me, Entry{Verdict: "allow", Reason: "after the pipe"})
	require.NoError(t, err)

	sink := &fakeSink{}
	done := make(chan error, 1)
	go func() { done <- Ingest(context.Background(), openRoot(t, root), DefaultLimits(), sink) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "the pass waits on the FIFO")
	}
	require.Len(t, sink.entries, 1)
	assert.Equal(t, "after the pipe", sink.entries[0].Reason)
	report := sink.reports[uint32(os.Getuid())]
	require.Len(t, report.Refused, 1)
	assert.Equal(t, "1-pipe.json", report.Refused[0].Name)
	assert.Contains(t, report.Refused[0].Reason, "not a regular file")
	assert.Empty(t, names(t, dir))
}
