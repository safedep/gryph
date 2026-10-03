//go:build windows

package account

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func currentID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read the current account: %w", err)
	}
	return tu.User.Sid.String(), nil
}
