package codex

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

var HookTypes = []string{
	"SessionStart",
	"PreToolUse",
	"PostToolUse",
	"UserPromptSubmit",
	"Stop",
}

type HooksConfig struct {
	Hooks map[string][]HookMatcher `json:"hooks"`
}

type HookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func hookMatcher(hookType string) string {
	switch hookType {
	case "PreToolUse", "PostToolUse":
		return "*"
	default:
		return ""
	}
}

func GenerateHooksConfig() *HooksConfig {
	config := &HooksConfig{
		Hooks: make(map[string][]HookMatcher),
	}

	for _, hookType := range HookTypes {
		config.Hooks[hookType] = []HookMatcher{
			{
				Matcher: hookMatcher(hookType),
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: fmt.Sprintf("%s _hook codex %s", utils.GryphCommand(), hookType),
						Timeout: 30,
					},
				},
			},
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
		result.Error = fmt.Errorf("codex not detected: %s", detection.Message)
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
		if !opts.Force && !opts.DryRun && hasGryphHooks(existingConfig) {
			result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
			result.Success = true
			return result, nil
		}

		if opts.Backup && !opts.DryRun {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "codex")
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

	for hookType, matchers := range gryphConfig.Hooks {
		found := false
		for _, m := range existing.Hooks[hookType] {
			for _, h := range m.Hooks {
				if h.Command == matchers[0].Hooks[0].Command {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			existing.Hooks[hookType] = append(existing.Hooks[hookType], matchers...)
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
		pattern := filepath.Join(opts.BackupDir, "codex", "hooks.json.backup.*")
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
		filtered := []HookMatcher{}
		for _, m := range config.Hooks[hookType] {
			filteredHooks := []HookCommand{}
			for _, h := range m.Hooks {
				if !utils.IsGryphCommand(h.Command) {
					filteredHooks = append(filteredHooks, h)
				}
			}
			if len(filteredHooks) > 0 {
				m.Hooks = filteredHooks
				filtered = append(filtered, m)
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

func hasGryphHooks(config *HooksConfig) bool {
	for _, hookType := range HookTypes {
		for _, m := range config.Hooks[hookType] {
			for _, h := range m.Hooks {
				if utils.IsGryphCommand(h.Command) {
					return true
				}
			}
		}
	}
	return false
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

	for _, hookType := range HookTypes {
		expectedCmd := fmt.Sprintf("%s _hook codex %s", utils.GryphCommand(), hookType)
		for _, m := range config.Hooks[hookType] {
			for _, h := range m.Hooks {
				if h.Command == expectedCmd {
					status.Installed = true
					status.Hooks = append(status.Hooks, hookType)
					break
				}
			}
		}
	}

	if status.Installed {
		for _, hookType := range HookTypes {
			found := false
			for _, h := range status.Hooks {
				if h == hookType {
					found = true
					break
				}
			}
			if !found {
				status.Valid = false
				status.Issues = append(status.Issues, fmt.Sprintf("%s: hook not configured", hookType))
			}
		}
	}

	return status, nil
}
