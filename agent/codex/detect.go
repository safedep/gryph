package codex

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
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(cmdCtx, "codex", "--version").Output()
	if err != nil {
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	if parts := strings.Fields(version); len(parts) > 0 {
		for _, part := range parts {
			if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
				return part
			}
		}
	}

	return version
}
