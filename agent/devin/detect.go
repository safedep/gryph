package devin

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

	devinDir := filepath.Join(home, ".config", "devin")

	if _, err := os.Stat(devinDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Devin CLI not installed (~/.config/devin not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       devinDir,
		ConfigPath: devinDir,
		HooksPath:  filepath.Join(devinDir, "config.json"),
	}

	result.Version = getVersion(ctx)

	return result, nil
}

func getVersion(ctx context.Context) string {
	if v := utils.ProgramVersion(ctx, "devin", "--version"); v != "" {
		return v
	}
	return "unknown"
}
