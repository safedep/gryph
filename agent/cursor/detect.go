package cursor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/safedep/gryph/agent"
)

// Detect checks if Cursor is installed on the system.
func Detect(ctx context.Context) (*agent.DetectionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "could not determine home directory",
		}, nil
	}

	cursorDir := filepath.Join(home, ".cursor")
	hooksFile := filepath.Join(cursorDir, "hooks.json")

	// Check for Cursor installation based on platform
	var installed bool
	var installPath string

	switch runtime.GOOS {
	case "darwin":
		// Check common macOS locations
		paths := []string{
			filepath.Join(home, "Applications", "Cursor.app"),
			"/Applications/Cursor.app",
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				installed = true
				installPath = p
				break
			}
		}

	case "linux":
		// Check if cursor is in PATH
		if path, err := exec.LookPath("cursor"); err == nil {
			installed = true
			installPath = path
		}

	case "windows":
		// Check Windows installation locations
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			cursorPath := filepath.Join(localAppData, "Programs", "cursor", "Cursor.exe")
			if _, err := os.Stat(cursorPath); err == nil {
				installed = true
				installPath = cursorPath
			}
		}
	}

	if !installed {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Cursor not installed",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       installPath,
		ConfigPath: cursorDir,
		HooksPath:  hooksFile,
	}

	// Try to get version
	result.Version = getVersion(ctx)

	return result, nil
}

// getVersion attempts to get the Cursor version.
func getVersion(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "cursor", "--version")
	output, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
		// Extract version number if it's in a longer string
		if parts := strings.Fields(version); len(parts) > 0 {
			for _, part := range parts {
				if len(part) > 0 && (part[0] >= '0' && part[0] <= '9') {
					return part
				}
			}
		}
		return version
	}
	return "unknown"
}
