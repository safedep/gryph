package codex

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

	codexDir := filepath.Join(home, ".codex")

	if _, err := os.Stat(codexDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Codex not installed (~/.codex not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       codexDir,
		ConfigPath: codexDir,
		HooksPath:  filepath.Join(codexDir, "hooks.json"),
	}

	result.Version = getVersion(ctx)

	return result, nil
}

func getVersion(ctx context.Context) string {
	if v := utils.ProgramVersion(ctx, "codex", "--version"); v != "" {
		return v
	}
	return "unknown"
}
