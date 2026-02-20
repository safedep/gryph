package piagent

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

	piDir := filepath.Join(home, ".pi", "agent")

	if _, err := os.Stat(piDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "pi agent not installed (~/.pi/agent not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       piDir,
		ConfigPath: piDir,
		HooksPath:  filepath.Join(piDir, "extensions"),
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(cmdCtx, "pi", "--version").Output(); err == nil {
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
