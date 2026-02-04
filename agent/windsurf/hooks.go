package windsurf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
)

var HookTypes = []string{
	"pre_read_code",
	"post_read_code",
	"pre_write_code",
	"post_write_code",
	"pre_run_command",
	"post_run_command",
	"pre_mcp_tool_use",
	"post_mcp_tool_use",
	"pre_user_prompt",
	"post_cascade_response",
	"post_setup_worktree",
}

type HooksConfig struct {
	Hooks map[string][]HookCommand `json:"hooks"`
}

type HookCommand struct {
	Command string `json:"command"`
}

func GenerateHooksConfig() *HooksConfig {
	config := &HooksConfig{
		Hooks: make(map[string][]HookCommand),
	}

	for _, hookType := range HookTypes {
		config.Hooks[hookType] = []HookCommand{
			{Command: fmt.Sprintf("gryph _hook windsurf %s", hookType)},
		}
	}

	return config
}

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
		result.Error = fmt.Errorf("failed to detect Windsurf: %w", err)
		return result, result.Error
	}

	configDir := detection.ConfigPath
	hooksFile := detection.HooksPath

	if !opts.DryRun {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			result.Error = fmt.Errorf("failed to create config directory: %w", err)
			return result, result.Error
		}
	}

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
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "windsurf")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("hooks.json.backup.%s", time.Now().Format("20060102150405")))
				}
			} else {
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
			existingConfig = mergeHooksConfig(existingConfig)
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

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

	status, err := GetHookStatus(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("verification failed: %v", err))
	} else if !status.Valid {
		result.Warnings = append(result.Warnings, "hooks installed but validation failed")
		result.Warnings = append(result.Warnings, status.Issues...)
	}

	result.Success = true
	return result, nil
}

func mergeHooksConfig(existing *HooksConfig) *HooksConfig {
	gryphConfig := GenerateHooksConfig()

	for hookType, commands := range gryphConfig.Hooks {
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

	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "windsurf", "hooks.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
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

func isGryphCommand(cmd string) bool {
	return len(cmd) >= 5 && cmd[:5] == "gryph"
}

func ValidateHooksContent(config *HooksConfig) []string {
	var issues []string

	for _, hookType := range HookTypes {
		expectedCmd := fmt.Sprintf("gryph _hook windsurf %s", hookType)
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

	if status.Installed {
		issues := ValidateHooksContent(&config)
		if len(issues) > 0 {
			status.Valid = false
			status.Issues = append(status.Issues, issues...)
		}
	}

	return status, nil
}
