package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTerminal struct {
	io.Reader
	out strings.Builder
}

func (f *fakeTerminal) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeTerminal) Close() error                { return nil }

// The confirmation reads the terminal, never stdin. A yes is y or yes.
// No terminal is no.
func TestConfirmOnTerminal(t *testing.T) {
	saved := openTerminal
	t.Cleanup(func() { openTerminal = saved })
	for reply, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false} {
		term := &fakeTerminal{Reader: strings.NewReader(reply)}
		openTerminal = func() (io.ReadWriteCloser, error) { return term, nil }
		ok, err := confirmOnTerminal("Answer? [y/N]: ")
		require.NoError(t, err)
		assert.Equal(t, want, ok, "reply %q", reply)
		assert.Equal(t, "Answer? [y/N]: ", term.out.String())
	}
	openTerminal = func() (io.ReadWriteCloser, error) { return nil, errors.New("no tty") }
	ok, err := confirmOnTerminal("Answer? [y/N]: ")
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "this command has none")
}
