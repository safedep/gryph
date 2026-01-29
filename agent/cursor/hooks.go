package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/safedep/gryph/agent"
)

// HookTypes are the hook types supported by Cursor.
var HookTypes = []string{
	"beforeSubmitPrompt",
	"beforeShellExecution",
	"beforeMCPExecution",
	"beforeReadFile",
	"afterFileEdit",
	"stop",
}

// HooksConfig represents the Cursor hooks.json structure.
type HooksConfig struct {
	Version int                       `json:"version"`
	Hooks   map[string][]HookCommand  `json:"hooks"`
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
			{Command: fmt.Sprintf("gryph _hook cursor %s", hookType)},
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
		result.Error = fmt.Errorf("Cursor is not installed")
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
		if opts.Backup && !opts.DryRun {
			backupPath := fmt.Sprintf("%s.backup.%s", hooksFile, time.Now().Format("20060102150405"))
			data, _ := json.MarshalIndent(existingConfig, "", "  ")
			if err := os.WriteFile(backupPath, data, 0600); err == nil {
				result.BackupPaths["hooks.json"] = backupPath
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to backup hooks.json: %v", err))
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
	result.Success = true
	result.Warnings = append(result.Warnings, "Note: Limited hook support. Some actions may not be logged.")

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

	// Remove gryph commands from each hook type
	for hookType := range config.Hooks {
		filtered := []HookCommand{}
		for _, cmd := range config.Hooks[hookType] {
			if !isGryphCommand(cmd.Command) {
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

// isGryphCommand checks if a command is a gryph command.
func isGryphCommand(cmd string) bool {
	return len(cmd) >= 5 && cmd[:5] == "gryph"
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
			if isGryphCommand(cmd.Command) {
				status.Installed = true
				status.Hooks = append(status.Hooks, hookType)
				break
			}
		}
	}

	return status, nil
}
