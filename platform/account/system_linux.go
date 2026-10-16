package account

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// createSystem runs useradd.
func createSystem(ctx context.Context, name, home string) error {
	shell := "/usr/sbin/nologin"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/false"
	}
	out, err := exec.CommandContext(ctx, "useradd", "--system", "--no-create-home", "--home-dir", home, "--shell", shell, "--user-group", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("useradd %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
