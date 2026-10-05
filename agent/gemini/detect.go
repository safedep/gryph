package gemini

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

	geminiDir := filepath.Join(home, ".gemini")

	if _, err := os.Stat(geminiDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Gemini CLI not installed (~/.gemini not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       geminiDir,
		ConfigPath: geminiDir,
		HooksPath:  filepath.Join(geminiDir, "hooks"),
	}

	result.Version = utils.ProgramVersion(ctx, "gemini", "--version")

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
