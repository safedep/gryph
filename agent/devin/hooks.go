package devin

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
	"SessionEnd",
}

// HookMatcher mirrors the Devin CLI hooks config structure. It is the same
// JSON format Claude Code and Codex use.
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

func gryphHookCommand(hookType string) string {
	return fmt.Sprintf("%s _hook devin %s", utils.GryphCommand(), hookType)
}

// readDevinConfig reads the full Devin config.json as a raw JSON map. This
// lets the installer merge the hooks section without touching unrelated
// settings. Devin has one config file, unlike Codex which owns a dedicated
// hooks.json.
func readDevinConfig(configFile string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config.json is malformed: %w", err)
	}

	return raw, nil
}

func readHooksFromConfig(raw map[string]json.RawMessage) (map[string][]HookMatcher, error) {
	hooksRaw, ok := raw["hooks"]
	if !ok {
		return make(map[string][]HookMatcher), nil
	}

	var hooks map[string][]HookMatcher
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		return nil, fmt.Errorf("hooks section is malformed: %w", err)
	}

	if hooks == nil {
		hooks = make(map[string][]HookMatcher)
	}

	return hooks, nil
}

func writeHooksToConfig(raw map[string]json.RawMessage, hooks map[string][]HookMatcher) error {
	data, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("failed to marshal hooks: %w", err)
	}
	raw["hooks"] = json.RawMessage(data)
	return nil
}

func writeDevinConfig(configFile string, raw map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(configFile, data, 0600)
}

func generateGryphHooks() map[string][]HookMatcher {
	hooks := make(map[string][]HookMatcher)
	for _, hookType := range HookTypes {
		hooks[hookType] = []HookMatcher{
			{
				Matcher: hookMatcher(hookType),
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: gryphHookCommand(hookType),
						Timeout: 30,
					},
				},
			},
		}
	}
	return hooks
}

func hasGryphHooks(hooks map[string][]HookMatcher) bool {
	for _, hookType := range HookTypes {
		for _, m := range hooks[hookType] {
			for _, h := range m.Hooks {
				if utils.IsGryphCommand(h.Command) {
					return true
				}
			}
		}
	}
	return false
}

func mergeGryphHooks(existing map[string][]HookMatcher) map[string][]HookMatcher {
	gryph := generateGryphHooks()

	for hookType, matchers := range gryph {
		found := false
		for _, m := range existing[hookType] {
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
			existing[hookType] = append(existing[hookType], matchers...)
		}
	}

	return existing
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
		result.Error = fmt.Errorf("devin not detected: %s", detection.Message)
		return result, result.Error
	}

	configFile := detection.HooksPath

	if !opts.DryRun {
		if err := os.MkdirAll(detection.ConfigPath, 0700); err != nil {
			result.Error = fmt.Errorf("failed to create config directory: %w", err)
			return result, result.Error
		}
	}

	raw, err := readDevinConfig(configFile)
	if os.IsNotExist(err) {
		raw = make(map[string]json.RawMessage)
	} else if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("config.json is malformed, hooks section will be replaced: %v", err))
		raw = make(map[string]json.RawMessage)
	}

	existingHooks, err := readHooksFromConfig(raw)
	if err != nil {
		result.Warnings = append(result.Warnings, "hooks section is malformed, will be replaced")
		existingHooks = nil
	}

	if existingHooks != nil {
		if !opts.Force && !opts.DryRun && hasGryphHooks(existingHooks) {
			result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
			result.Success = true
			return result, nil
		}

		if opts.Backup && !opts.DryRun {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "devin")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("config.json.backup.%s", time.Now().Format("20060102150405")))
				}
			} else {
				backupPath = fmt.Sprintf("%s.backup.%s", configFile, time.Now().Format("20060102150405"))
			}
			if backupPath != "" {
				if data, err := os.ReadFile(configFile); err == nil {
					if err := os.WriteFile(backupPath, data, 0600); err == nil {
						result.BackupPaths["config.json"] = backupPath
					} else {
						result.Warnings = append(result.Warnings, fmt.Sprintf("failed to backup config.json: %v", err))
					}
				}
			}
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

	var mergedHooks map[string][]HookMatcher
	if existingHooks != nil && !opts.Force {
		mergedHooks = mergeGryphHooks(existingHooks)
	} else {
		mergedHooks = generateGryphHooks()
	}

	if err := writeHooksToConfig(raw, mergedHooks); err != nil {
		result.Error = err
		return result, result.Error
	}

	if err := writeDevinConfig(configFile, raw); err != nil {
		result.Error = fmt.Errorf("failed to write config.json: %w", err)
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

	configFile := detection.HooksPath

	raw, err := readDevinConfig(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			result.Success = true
			return result, nil
		}
		result.Error = fmt.Errorf("failed to read config.json: %w", err)
		return result, result.Error
	}

	hooks, err := readHooksFromConfig(raw)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse hooks section: %w", err)
		return result, result.Error
	}

	if opts.DryRun {
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "devin", "config.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			backupPath := matches[len(matches)-1]
			if backupData, err := os.ReadFile(backupPath); err == nil {
				if err := os.WriteFile(configFile, backupData, 0600); err == nil {
					result.BackupsRestored = true
					result.HooksRemoved = HookTypes
					result.Success = true
					return result, nil
				}
			}
		}
	}

	for hookType := range hooks {
		filtered := []HookMatcher{}
		for _, m := range hooks[hookType] {
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
		hooks[hookType] = filtered
	}

	if err := writeHooksToConfig(raw, hooks); err != nil {
		result.Error = err
		return result, result.Error
	}

	if err := writeDevinConfig(configFile, raw); err != nil {
		result.Error = fmt.Errorf("failed to write config.json: %w", err)
		return result, result.Error
	}

	result.Success = true
	return result, nil
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

	raw, err := readDevinConfig(detection.HooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read config.json: %v", err))
		return status, nil
	}

	hooks, err := readHooksFromConfig(raw)
	if err != nil {
		status.Issues = append(status.Issues, "hooks section in config.json is malformed")
		return status, nil
	}

	status.Valid = true

	for _, hookType := range HookTypes {
		expectedCmd := gryphHookCommand(hookType)
		for _, m := range hooks[hookType] {
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
