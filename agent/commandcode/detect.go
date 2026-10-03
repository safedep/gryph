package commandcode

import (
	"context"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// Detect checks if Command Code is installed on the system.
func Detect(ctx context.Context) (*agent.DetectionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "could not determine home directory",
		}, nil
	}

	configDir := filepath.Join(home, ".commandcode")

	// Check if .commandcode directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Command Code not installed (~/.commandcode not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       configDir,
		ConfigPath: configDir,
		HooksPath:  filepath.Join(configDir, "hooks"),
	}

	// Try to get version from the command-code CLI. The CLI is also
	// distributed under the shorter aliases cmdc, commandcode and cmd, so
	// fall back through them before giving up.
	for _, bin := range []string{"command-code", "cmdc", "commandcode"} {
		if v := utils.ProgramVersion(ctx, bin, "--version"); v != "" {
			result.Version = v
			break
		}
	}

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
