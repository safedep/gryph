package commandcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// HookTypes are the hook types supported by Command Code that we want to
// capture. Command Code fires PreToolUse/PostToolUse around every tool call
// and Stop/SessionStart on turn and session boundaries.
var HookTypes = []string{
	"PreToolUse",
	"PostToolUse",
	"Stop",
	"SessionStart",
}

// SettingsHooks represents the hooks section in settings.json.
type SettingsHooks map[string][]HookMatcher

// HookMatcher represents a matcher entry for a hook type. Command Code
// treats the matcher as a regex tested against the tool display name, and
// matches every tool when it is omitted. Stop and SessionStart carry no
// tool, so a matcher must never be set for them.
type HookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand represents a hook command configuration.
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// GenerateHooksConfig generates the hooks configuration for gryph. Matchers
// are omitted so that every tool call is captured.
func GenerateHooksConfig() SettingsHooks {
	hooks := make(SettingsHooks)

	for _, hookType := range HookTypes {
		hooks[hookType] = []HookMatcher{
			{
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: expectedHookCommand(hookType),
					},
				},
			},
		}
	}

	return hooks
}

// expectedHookCommand returns the exact hook command gryph installs for the
// given hook type.
func expectedHookCommand(hookType string) string {
	return fmt.Sprintf("%s _hook command-code %s", utils.GryphCommand(), hookType)
}

// isOwnedHookCommand reports whether a hook command string was installed by
// gryph for the given hook type. Commands are compared field by field so
// that unrelated executables whose names merely start with "gryph" (e.g.
// gryphon, gryph-helper) and hooks installed for other agents are never
// treated as owned — and therefore never skipped or removed by gryph.
func isOwnedHookCommand(cmd, hookType string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 4 {
		return false
	}
	if filepath.Base(fields[0]) != utils.GryphCommand() {
		return false
	}
	return fields[1] == "_hook" && fields[2] == AgentName && fields[3] == hookType
}

// readSettings reads the settings.json file. A "hooks" key that is present
// but not a JSON object is rejected here so that install, uninstall, and
// status all fail with the same configuration error instead of silently
// treating a malformed section as empty.
func readSettings(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]interface{}), nil
	}
	if err != nil {
		return nil, err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// A top-level JSON null unmarshals to a nil map without error.
	if settings == nil {
		return nil, fmt.Errorf(`invalid settings in %s: expected an object`, path)
	}

	if hooks, ok := settings["hooks"]; ok && hooks != nil {
		if _, ok := hooks.(map[string]interface{}); !ok {
			return nil, fmt.Errorf(`invalid "hooks" section in %s: expected an object`, path)
		}
	}

	return settings, nil
}

// writeSettings writes the settings.json file.
func writeSettings(path string, settings map[string]interface{}) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// backupSettings copies the current settings file to a timestamped backup,
// either under backupDir (grouped per agent) or alongside the original.
// It returns the backup path on success; a failed backup is an error so the
// caller can abort instead of overwriting an untracked file.
func backupSettings(settingsPath, backupDir string) (string, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read existing settings.json for backup: %w", err)
	}

	var backupPath string
	if backupDir != "" {
		dir := filepath.Join(backupDir, "command-code")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create backup directory: %w", err)
		}
		backupPath = filepath.Join(dir, fmt.Sprintf("settings.json.backup.%s", time.Now().Format("20060102150405")))
	} else {
		backupPath = fmt.Sprintf("%s.backup.%s", settingsPath, time.Now().Format("20060102150405"))
	}

	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return "", fmt.Errorf("failed to write backup %s: %w", backupPath, err)
	}

	return backupPath, nil
}

// InstallHooks installs hooks for Command Code by modifying settings.json.
// Hook types that already carry gryph's command are left untouched; missing
// ones are added, so a partially configured setup is completed rather than
// rejected.
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
		result.Error = fmt.Errorf("Command Code is not installed: %s", detection.Message)
		return result, result.Error
	}

	settingsPath := filepath.Join(detection.ConfigPath, "settings.json")

	settings, err := readSettings(settingsPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to read settings.json: %w", err)
		return result, result.Error
	}

	// Back up the current file before touching it; a failed backup aborts
	// the install so we never overwrite settings we failed to preserve.
	if opts.Backup && !opts.DryRun {
		if _, err := os.Stat(settingsPath); err == nil {
			backupPath, err := backupSettings(settingsPath, opts.BackupDir)
			if err != nil {
				result.Error = err
				return result, result.Error
			}
			result.BackupPaths["settings.json"] = backupPath
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

	gryphHooks := GenerateHooksConfig()

	if settings["hooks"] == nil {
		settings["hooks"] = make(map[string]interface{})
	}
	hooksSection := settings["hooks"].(map[string]interface{})

	for hookType, matchers := range gryphHooks {
		if !opts.Force && hookSectionHasCommand(hooksSection, hookType) {
			continue
		}
		matchersData, err := json.Marshal(matchers)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to marshal matchers: %v", err))
			continue
		}

		var matchersInterface interface{}
		if err := json.Unmarshal(matchersData, &matchersInterface); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to unmarshal matchers: %v", err))
			continue
		}

		if opts.Force {
			hooksSection[hookType] = matchersInterface
		} else {
			if existing, ok := hooksSection[hookType].([]interface{}); ok {
				newMatchers, _ := matchersInterface.([]interface{})
				hooksSection[hookType] = append(existing, newMatchers...)
			} else {
				hooksSection[hookType] = matchersInterface
			}
		}

		result.HooksInstalled = append(result.HooksInstalled, hookType)
	}

	if len(result.HooksInstalled) == 0 {
		result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
	}

	if err := writeSettings(settingsPath, settings); err != nil {
		result.Error = fmt.Errorf("failed to write settings.json: %w", err)
		return result, result.Error
	}

	// Verify installation
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

// hookSectionHasCommand reports whether the hooks section already contains
// gryph's command for the given hook type.
func hookSectionHasCommand(hooksSection map[string]interface{}, hookType string) bool {
	matchers, ok := hooksSection[hookType].([]interface{})
	if !ok {
		return false
	}

	for _, m := range matchers {
		matcher, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		hooksList, ok := matcher["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksList {
			hook, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, _ := hook["command"].(string); isOwnedHookCommand(cmd, hookType) {
				return true
			}
		}
	}

	return false
}

// UninstallHooks removes hooks from Command Code. Only commands gryph
// installed for this agent are removed; user commands in the same hook type
// are preserved and the type is still reported in HooksRemoved.
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

	settingsPath := filepath.Join(detection.ConfigPath, "settings.json")

	// Restore the most recent backup when explicitly requested. A failed or
	// missing restoration is an error; the normal removal path must not run
	// afterwards and the operation must not report success.
	if opts.RestoreBackup {
		if opts.BackupDir == "" {
			result.Error = fmt.Errorf("backup restoration requested but no backup directory was provided")
			return result, result.Error
		}

		pattern := filepath.Join(opts.BackupDir, "command-code", "settings.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) == 0 {
			result.Error = fmt.Errorf("no backup found in %s", filepath.Dir(pattern))
			return result, result.Error
		}

		backupPath := matches[len(matches)-1]
		data, err := os.ReadFile(backupPath)
		if err != nil {
			result.Error = fmt.Errorf("failed to read backup %s: %w", backupPath, err)
			return result, result.Error
		}

		if !opts.DryRun {
			if err := os.WriteFile(settingsPath, data, 0600); err != nil {
				result.Error = fmt.Errorf("failed to restore settings.json from %s: %w", backupPath, err)
				return result, result.Error
			}
		}

		result.BackupsRestored = true
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	settings, err := readSettings(settingsPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to read settings.json: %w", err)
		return result, result.Error
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		result.Success = true
		return result, nil
	}

	if opts.DryRun {
		for _, hookType := range HookTypes {
			if _, exists := hooks[hookType]; exists {
				result.HooksRemoved = append(result.HooksRemoved, hookType)
			}
		}
		result.Success = true
		return result, nil
	}

	// Remove gryph-owned commands from each hook type, preserving user hooks.
	for _, hookType := range HookTypes {
		matchers, ok := hooks[hookType].([]interface{})
		if !ok {
			continue
		}

		filtered := []interface{}{}
		removedAny := false

		for _, m := range matchers {
			matcher, ok := m.(map[string]interface{})
			if !ok {
				filtered = append(filtered, m)
				continue
			}

			hooksList, ok := matcher["hooks"].([]interface{})
			if !ok {
				filtered = append(filtered, m)
				continue
			}

			filteredHooks := []interface{}{}
			for _, h := range hooksList {
				hook, ok := h.(map[string]interface{})
				if !ok {
					filteredHooks = append(filteredHooks, h)
					continue
				}
				cmd, _ := hook["command"].(string)
				if isOwnedHookCommand(cmd, hookType) {
					removedAny = true
					continue
				}
				filteredHooks = append(filteredHooks, h)
			}

			if len(filteredHooks) > 0 {
				matcher["hooks"] = filteredHooks
				filtered = append(filtered, matcher)
			}
		}

		if len(filtered) == 0 {
			delete(hooks, hookType)
		} else if removedAny {
			hooks[hookType] = filtered
		}

		// A hook type that lost at least one gryph command counts as removed
		// even when user commands remain in it.
		if removedAny {
			result.HooksRemoved = append(result.HooksRemoved, hookType)
		}
	}

	if len(result.HooksRemoved) == 0 {
		result.Success = true
		return result, nil
	}

	// Write updated settings
	if err := writeSettings(settingsPath, settings); err != nil {
		result.Error = fmt.Errorf("failed to write settings.json: %w", err)
		return result, result.Error
	}

	result.Success = true
	return result, nil
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

	settingsPath := filepath.Join(detection.ConfigPath, "settings.json")
	settings, err := readSettings(settingsPath)
	if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read settings.json: %v", err))
		return status, nil
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return status, nil
	}

	status.Valid = true

	for _, hookType := range HookTypes {
		matchers, ok := hooks[hookType].([]interface{})
		if !ok {
			continue
		}

		for _, m := range matchers {
			matcher, ok := m.(map[string]interface{})
			if !ok {
				continue
			}

			hooksList, ok := matcher["hooks"].([]interface{})
			if !ok {
				continue
			}

			for _, h := range hooksList {
				hook, ok := h.(map[string]interface{})
				if !ok {
					continue
				}
				cmd, _ := hook["command"].(string)
				if isOwnedHookCommand(cmd, hookType) {
					status.Installed = true
					status.Hooks = append(status.Hooks, hookType)
					break
				}
			}
		}
	}

	// Validate that all expected hooks are installed
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
