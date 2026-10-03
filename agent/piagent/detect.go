package piagent

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

	result.Version = utils.ProgramVersion(ctx, "pi", "--version")

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
