package openclaw

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

	openclawDir := filepath.Join(home, ".openclaw")

	if _, err := os.Stat(openclawDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "OpenClaw not installed (~/.openclaw not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       openclawDir,
		ConfigPath: openclawDir,
		HooksPath:  filepath.Join(openclawDir, "extensions"),
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(cmdCtx, "openclaw", "--version").Output(); err == nil {
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
