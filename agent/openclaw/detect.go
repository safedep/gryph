package openclaw

import (
	"context"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
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

	result.Version = utils.ProgramVersion(ctx, "openclaw", "--version")

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
