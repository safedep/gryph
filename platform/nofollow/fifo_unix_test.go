//go:build !windows

package nofollow

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestDir_OpenDoesNotWaitOnFIFO(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, unix.Mkfifo(filepath.Join(base, "pipe"), 0o600))
	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := d.Open("pipe")
		done <- err
	}()
	select {
	case err := <-done:
		assert.Error(t, err, "a FIFO is not a regular file")
	case <-time.After(5 * time.Second):
		assert.Fail(t, "the open waits on the FIFO")
	}
	_, err = os.Lstat(filepath.Join(base, "pipe"))
	assert.NoError(t, err)
}
