package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/safedep/gryph/agent"
)

// Detect checks if Claude Code is installed on the system.
func Detect(ctx context.Context) (*agent.DetectionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "could not determine home directory",
		}, nil
	}

	claudeDir := filepath.Join(home, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks")

	// Check if .claude directory exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Claude Code not installed (~/.claude not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       claudeDir,
		ConfigPath: claudeDir,
		HooksPath:  hooksDir,
	}

	// Try to get version from claude CLI
	if output, err := exec.CommandContext(ctx, "claude", "-v").Output(); err == nil {
		// Output format: "2.1.15 (Claude Code)"
		version := strings.TrimSpace(string(output))
		if idx := strings.Index(version, " "); idx > 0 {
			version = version[:idx]
		}
		result.Version = version
	}

	// Fallback: try settings.json
	if result.Version == "" {
		settingsPath := filepath.Join(claudeDir, "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var settings map[string]interface{}
			if err := json.Unmarshal(data, &settings); err == nil {
				if version, ok := settings["version"].(string); ok {
					result.Version = version
				}
			}
		}
	}

	if result.Version == "" {
		result.Version = "unknown"
	}

	return result, nil
}
