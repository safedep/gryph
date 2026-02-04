package windsurf

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

func Detect(ctx context.Context) (*agent.DetectionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "could not determine home directory",
		}, nil
	}

	configDir := filepath.Join(home, ".codeium", "windsurf")
	hooksFile := filepath.Join(configDir, "hooks.json")

	var installed bool
	var installPath string

	switch runtime.GOOS {
	case "darwin":
		paths := []string{
			"/Applications/Windsurf.app",
			filepath.Join(home, "Applications", "Windsurf.app"),
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				installed = true
				installPath = p
				break
			}
		}

	case "linux":
		if path, err := exec.LookPath("windsurf"); err == nil {
			installed = true
			installPath = path
		}
	}

	// Fallback: check if config directory exists
	if !installed {
		if _, err := os.Stat(configDir); err == nil {
			installed = true
			installPath = configDir
		}
	}

	if !installed {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Windsurf not installed",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       installPath,
		ConfigPath: configDir,
		HooksPath:  hooksFile,
	}

	result.Version = getVersion(ctx)

	return result, nil
}

func getVersion(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "windsurf", "--version")
	output, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
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
