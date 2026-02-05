package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// HookTypes are the hook types supported by Cursor that we install.
// These are the hooks that Gryph registers for monitoring.
var HookTypes = []string{
	// Pre-action hooks (can block)
	"preToolUse",
	"beforeShellExecution",
	"beforeMCPExecution",
	"beforeReadFile",
	"beforeTabFileRead",
	"beforeSubmitPrompt",

	// Post-action hooks (for logging)
	"postToolUse",
	"postToolUseFailure",
	"afterFileEdit",
	"afterTabFileEdit",
	"afterShellExecution",
	"afterMCPExecution",
	"afterAgentResponse",
	"afterAgentThought",

	// Session lifecycle hooks
	"sessionStart",
	"sessionEnd",
	"stop",

	// Subagent hooks
	"subagentStart",
	"subagentStop",

	// Other hooks
	"preCompact",
}

// HooksConfig represents the Cursor hooks.json structure.
type HooksConfig struct {
	Version int                      `json:"version"`
	Hooks   map[string][]HookCommand `json:"hooks"`
}

// HookCommand represents a single hook command.
type HookCommand struct {
	Command string `json:"command"`
}

// GenerateHooksConfig generates the hooks.json content for Gryph.
func GenerateHooksConfig() *HooksConfig {
	config := &HooksConfig{
		Version: 1,
		Hooks:   make(map[string][]HookCommand),
	}

	for _, hookType := range HookTypes {
		config.Hooks[hookType] = []HookCommand{
			{Command: fmt.Sprintf("%s _hook cursor %s", utils.GryphCommand(), hookType)},
		}
	}

	return config
}

// InstallHooks installs hooks for Cursor.
func InstallHooks(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	result := &agent.InstallResult{
		BackupPaths: make(map[string]string),
	}

	detection, err := Detect(ctx)
	if err != nil {
		result.Error = err
		return result, err
	}

	if !detection.Installed {
		result.Error = fmt.Errorf("failed to detect Cursor: %w", err)
		return result, result.Error
	}

	configDir := detection.ConfigPath
	hooksFile := detection.HooksPath

	// Create config directory if it doesn't exist
	if !opts.DryRun {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			result.Error = fmt.Errorf("failed to create config directory: %w", err)
			return result, result.Error
		}
	}

	// Check if hooks.json already exists
	var existingConfig *HooksConfig
	if data, err := os.ReadFile(hooksFile); err == nil {
		existingConfig = &HooksConfig{}
		if err := json.Unmarshal(data, existingConfig); err != nil {
			result.Warnings = append(result.Warnings, "existing hooks.json is malformed, will be replaced")
			existingConfig = nil
		}
	}

	if existingConfig != nil {
		if !opts.Force && !opts.DryRun && hasGryphHooks(existingConfig) {
			result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
			result.Success = true
			result.Warnings = append(result.Warnings, "Limited hook support. Some actions may not be logged.")
			return result, nil
		}

		if opts.Backup && !opts.DryRun {
			var backupPath string
			if opts.BackupDir != "" {
				// Use centralized backup directory
				backupDir := filepath.Join(opts.BackupDir, "cursor")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("hooks.json.backup.%s", time.Now().Format("20060102150405")))
				}
			} else {
				// Fallback to inline backup
				backupPath = fmt.Sprintf("%s.backup.%s", hooksFile, time.Now().Format("20060102150405"))
			}
			if backupPath != "" {
				data, _ := json.MarshalIndent(existingConfig, "", "  ")
				if err := os.WriteFile(backupPath, data, 0600); err == nil {
					result.BackupPaths["hooks.json"] = backupPath
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to backup hooks.json: %v", err))
				}
			}
		}

		if !opts.Force && !opts.DryRun {
			// Merge with existing config
			existingConfig = mergeHooksConfig(existingConfig)
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

	// Generate and write new config
	var newConfig *HooksConfig
	if existingConfig != nil && !opts.Force {
		newConfig = existingConfig
	} else {
		newConfig = GenerateHooksConfig()
	}

	data, err := json.MarshalIndent(newConfig, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal hooks config: %w", err)
		return result, result.Error
	}

	if err := os.WriteFile(hooksFile, data, 0600); err != nil {
		result.Error = fmt.Errorf("failed to write hooks.json: %w", err)
		return result, result.Error
	}

	result.HooksInstalled = HookTypes

	// Verify installation
	status, err := GetHookStatus(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("verification failed: %v", err))
	} else if !status.Valid {
		result.Warnings = append(result.Warnings, "hooks installed but validation failed")
		result.Warnings = append(result.Warnings, status.Issues...)
	}

	result.Success = true
	result.Warnings = append(result.Warnings, "Limited hook support. Some actions may not be logged.")

	return result, nil
}

// mergeHooksConfig merges Gryph hooks with existing hooks.
func mergeHooksConfig(existing *HooksConfig) *HooksConfig {
	gryphConfig := GenerateHooksConfig()

	for hookType, commands := range gryphConfig.Hooks {
		// Check if gryph command already exists
		found := false
		for _, cmd := range existing.Hooks[hookType] {
			if cmd.Command == commands[0].Command {
				found = true
				break
			}
		}
		if !found {
			existing.Hooks[hookType] = append(existing.Hooks[hookType], commands...)
		}
	}

	return existing
}

// UninstallHooks removes hooks from Cursor.
func UninstallHooks(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	result := &agent.UninstallResult{}

	detection, err := Detect(ctx)
	if err != nil {
		result.Error = err
		return result, err
	}

	if !detection.Installed {
		result.Success = true
		return result, nil
	}

	hooksFile := detection.HooksPath

	data, err := os.ReadFile(hooksFile)
	if os.IsNotExist(err) {
		result.Success = true
		return result, nil
	} else if err != nil {
		result.Error = fmt.Errorf("failed to read hooks.json: %w", err)
		return result, result.Error
	}

	var config HooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		result.Error = fmt.Errorf("failed to parse hooks.json: %w", err)
		return result, result.Error
	}

	if opts.DryRun {
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	// Check if we should restore backup
	if opts.RestoreBackup && opts.BackupDir != "" {
		// Look for backup files
		pattern := filepath.Join(opts.BackupDir, "cursor", "hooks.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			// Use most recent backup (last in sorted list)
			backupPath := matches[len(matches)-1]
			if backupData, err := os.ReadFile(backupPath); err == nil {
				if err := os.WriteFile(hooksFile, backupData, 0600); err == nil {
					result.BackupsRestored = true
					result.HooksRemoved = HookTypes
					result.Success = true
					return result, nil
				}
			}
		}
	}

	// Remove gryph commands from each hook type
	for hookType := range config.Hooks {
		filtered := []HookCommand{}
		for _, cmd := range config.Hooks[hookType] {
			if !utils.IsGryphCommand(cmd.Command) {
				filtered = append(filtered, cmd)
			} else {
				result.HooksRemoved = append(result.HooksRemoved, hookType)
			}
		}
		config.Hooks[hookType] = filtered
	}

	// Write updated config
	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal hooks config: %w", err)
		return result, result.Error
	}

	if err := os.WriteFile(hooksFile, newData, 0600); err != nil {
		result.Error = fmt.Errorf("failed to write hooks.json: %w", err)
		return result, result.Error
	}

	result.Success = true
	return result, nil
}

func hasGryphHooks(config *HooksConfig) bool {
	for _, hookType := range HookTypes {
		for _, cmd := range config.Hooks[hookType] {
			if utils.IsGryphCommand(cmd.Command) {
				return true
			}
		}
	}
	return false
}

// ValidateHooksContent checks if hooks.json contains correctly formatted gryph commands.
func ValidateHooksContent(config *HooksConfig) []string {
	var issues []string

	for _, hookType := range HookTypes {
		expectedCmd := fmt.Sprintf("%s _hook cursor %s", utils.GryphCommand(), hookType)
		found := false

		for _, cmd := range config.Hooks[hookType] {
			if cmd.Command == expectedCmd {
				found = true
				break
			}
		}

		if !found {
			issues = append(issues, fmt.Sprintf("%s: missing or incorrect gryph command", hookType))
		}
	}

	return issues
}

// GetHookStatus checks the current hook state.
func GetHookStatus(ctx context.Context) (*agent.HookStatus, error) {
	status := &agent.HookStatus{}

	detection, err := Detect(ctx)
	if err != nil {
		return status, err
	}

	if !detection.Installed {
		return status, nil
	}

	hooksFile := detection.HooksPath

	data, err := os.ReadFile(hooksFile)
	if os.IsNotExist(err) {
		return status, nil
	} else if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read hooks.json: %v", err))
		return status, nil
	}

	var config HooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		status.Issues = append(status.Issues, "hooks.json is malformed")
		return status, nil
	}

	status.Valid = true

	for hookType, commands := range config.Hooks {
		for _, cmd := range commands {
			if utils.IsGryphCommand(cmd.Command) {
				status.Installed = true
				status.Hooks = append(status.Hooks, hookType)
				break
			}
		}
	}

	// Validate content if hooks are installed
	if status.Installed {
		issues := ValidateHooksContent(&config)
		if len(issues) > 0 {
			status.Valid = false
			status.Issues = append(status.Issues, issues...)
		}
	}

	return status, nil
}
