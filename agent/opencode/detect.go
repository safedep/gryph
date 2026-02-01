package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	opencodeDir := filepath.Join(home, ".config", "opencode")

	if _, err := os.Stat(opencodeDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "OpenCode not installed (~/.config/opencode not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       opencodeDir,
		ConfigPath: opencodeDir,
		HooksPath:  filepath.Join(opencodeDir, "plugins"),
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(cmdCtx, "opencode", "--version").Output(); err == nil {
		version := strings.TrimSpace(string(output))
		if idx := strings.Index(version, " "); idx > 0 {
			version = version[:idx]
		}
		result.Version = version
	}

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
