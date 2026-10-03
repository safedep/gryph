//go:build windows

package account

import (
	"context"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

func currentID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read the current account: %w", err)
	}
	return tu.User.Sid.String(), nil
}

// list is not implemented on Windows: the profiles of the host come from
// the registry and a process cannot drop to another account without its
// credentials.
func list() ([]Account, error) {
	return nil, ErrUnsupported
}

func command(context.Context, Account, string, ...string) (*exec.Cmd, error) {
	return nil, ErrUnsupported
}

func lookup(string) (Account, error) {
	return Account{}, ErrUnsupported
}

func isSystemID(string) bool {
	return false
}

func lookupID(string) (Account, error) {
	return Account{}, ErrUnsupported
}
