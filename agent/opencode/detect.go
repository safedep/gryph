package opencode

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

	result.Version = utils.ProgramVersion(ctx, "opencode", "--version")

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
