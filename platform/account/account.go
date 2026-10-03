// Package account identifies the operating-system accounts of the host. The
// identifier of an account is stable across runs and unique on the host, so
// Gryph can derive per-account state from it, such as the system session
// that holds tamper events. Each platform supplies its own form: the numeric
// uid on Unix, the SID on Windows. Callers treat it as an opaque string.
package account

import (
	"context"
	"errors"
	"os/exec"
)

// ErrUnsupported says that the platform cannot list accounts or run a
// command as another account.
var ErrUnsupported = errors.New("not supported on this platform")

// Account is one human account of the host with a home directory.
type Account struct {
	// ID is the opaque identifier, the same form CurrentID returns.
	ID   string
	Name string
	Home string
}

// CurrentID returns the identifier of the account that runs this process.
func CurrentID() (string, error) {
	return currentID()
}

// List returns the human accounts of the host that have a home directory
// on disk. System accounts are left out. It reads the account database of
// the platform and runs no program.
func List() ([]Account, error) {
	return list()
}

// Command returns a command that runs program as the account, with the
// account's home as HOME and a minimal environment. The process drops to
// the account's uid and gid before it opens any file, so a link in the
// account's home reaches only what the account can already write. Only
// root can call it.
func Command(ctx context.Context, acct Account, program string, args ...string) (*exec.Cmd, error) {
	return command(ctx, acct, program, args...)
}
