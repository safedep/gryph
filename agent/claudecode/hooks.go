package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
)

// HookTypes are the hook types supported by Claude Code that we want to capture.
var HookTypes = []string{
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"SessionStart",
	"SessionEnd",
	"Notification",
}

// SettingsHooks represents the hooks section in settings.json.
type SettingsHooks map[string][]HookMatcher

// HookMatcher represents a matcher entry for a hook type.
type HookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand represents a hook command configuration.
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// Settings represents the Claude Code settings.json structure.
type Settings struct {
	Hooks map[string][]HookMatcher `json:"hooks,omitempty"`
	// Other fields preserved during read/write
	Other map[string]interface{} `json:"-"`
}

// GenerateHooksConfig generates the hooks configuration for gryph.
func GenerateHooksConfig() SettingsHooks {
	hooks := make(SettingsHooks)

	for _, hookType := range HookTypes {
		matcher := HookMatcher{
			Hooks: []HookCommand{
				{
					Type:    "command",
					Command: fmt.Sprintf("gryph _hook claude-code %s", hookType),
				},
			},
		}

		// PreToolUse, PostToolUse, PostToolUseFailure use matcher for tool filtering
		// We use "*" to match all tools
		if hookType == "PreToolUse" || hookType == "PostToolUse" || hookType == "PostToolUseFailure" {
			matcher.Matcher = "*"
		}

		hooks[hookType] = []HookMatcher{matcher}
	}

	return hooks
}

// readSettings reads the settings.json file.
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

// InstallHooks installs hooks for Claude Code by modifying settings.json.
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
		result.Error = fmt.Errorf("Claude Code is not installed")
		return result, result.Error
	}

	settingsPath := filepath.Join(detection.ConfigPath, "settings.json")

	// Read existing settings
	settings, err := readSettings(settingsPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to read settings.json: %w", err)
		return result, result.Error
	}

	// Check if hooks already exist
	existingHooks, hasHooks := settings["hooks"].(map[string]interface{})
	if hasHooks && !opts.Force && !opts.DryRun {
		// Check if gryph hooks are already installed
		if hasGryphHooks(existingHooks) {
			result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
			result.Success = true
			return result, nil
		}
	}

	// Backup existing settings if requested
	if opts.Backup && !opts.DryRun {
		if _, err := os.Stat(settingsPath); err == nil {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "claude-code")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("settings.json.backup.%s", time.Now().Format("20060102150405")))
					if data, err := os.ReadFile(settingsPath); err == nil {
						if err := os.WriteFile(backupPath, data, 0600); err == nil {
							result.BackupPaths["settings.json"] = backupPath
						}
					}
				}
			} else {
				backupPath = fmt.Sprintf("%s.backup.%s", settingsPath, time.Now().Format("20060102150405"))
				if data, err := os.ReadFile(settingsPath); err == nil {
					if err := os.WriteFile(backupPath, data, 0600); err == nil {
						result.BackupPaths["settings.json"] = backupPath
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

	// Generate gryph hooks config
	gryphHooks := GenerateHooksConfig()

	// Merge or replace hooks
	if settings["hooks"] == nil {
		settings["hooks"] = make(map[string]interface{})
	}
	hooksSection := settings["hooks"].(map[string]interface{})

	for hookType, matchers := range gryphHooks {
		// Convert matchers to interface{} for JSON
		matchersData, err := json.Marshal(matchers)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to marshal matchers: %v", err))
		}

		var matchersInterface interface{}
		if err := json.Unmarshal(matchersData, &matchersInterface); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to unmarshal matchers: %v", err))
		}

		if opts.Force {
			// Replace existing hooks for this type
			hooksSection[hookType] = matchersInterface
		} else {
			// Merge with existing hooks
			if existing, ok := hooksSection[hookType].([]interface{}); ok {
				// Append gryph matchers to existing
				newMatchers, _ := matchersInterface.([]interface{})
				hooksSection[hookType] = append(existing, newMatchers...)
			} else {
				hooksSection[hookType] = matchersInterface
			}
		}

		result.HooksInstalled = append(result.HooksInstalled, hookType)
	}

	// Write updated settings
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

// hasGryphHooks checks if gryph hooks are already installed.
func hasGryphHooks(hooks map[string]interface{}) bool {
	for _, hookType := range HookTypes {
		if matchers, ok := hooks[hookType].([]interface{}); ok {
			for _, m := range matchers {
				if matcher, ok := m.(map[string]interface{}); ok {
					if hooksList, ok := matcher["hooks"].([]interface{}); ok {
						for _, h := range hooksList {
							if hook, ok := h.(map[string]interface{}); ok {
								if cmd, ok := hook["command"].(string); ok {
									if len(cmd) >= 5 && cmd[:5] == "gryph" {
										return true
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return false
}

// UninstallHooks removes hooks from Claude Code.
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

	// Check if we should restore backup
	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "claude-code", "settings.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			backupPath := matches[len(matches)-1]
			if data, err := os.ReadFile(backupPath); err == nil {
				if !opts.DryRun {
					if err := os.WriteFile(settingsPath, data, 0600); err == nil {
						result.BackupsRestored = true
						result.HooksRemoved = HookTypes
						result.Success = true
						return result, nil
					}
				} else {
					result.HooksRemoved = HookTypes
					result.Success = true
					return result, nil
				}
			}
		}
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

	// Remove gryph hooks from each hook type
	for hookType := range hooks {
		matchers, ok := hooks[hookType].([]interface{})
		if !ok {
			continue
		}

		filtered := []interface{}{}
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

			// Filter out gryph commands
			filteredHooks := []interface{}{}
			for _, h := range hooksList {
				hook, ok := h.(map[string]interface{})
				if !ok {
					filteredHooks = append(filteredHooks, h)
					continue
				}
				cmd, _ := hook["command"].(string)
				if len(cmd) < 5 || cmd[:5] != "gryph" {
					filteredHooks = append(filteredHooks, h)
				}
			}

			if len(filteredHooks) > 0 {
				matcher["hooks"] = filteredHooks
				filtered = append(filtered, matcher)
			}
		}

		if len(filtered) > 0 {
			hooks[hookType] = filtered
		} else {
			delete(hooks, hookType)
			result.HooksRemoved = append(result.HooksRemoved, hookType)
		}
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
				expectedCmd := fmt.Sprintf("gryph _hook claude-code %s", hookType)
				if cmd == expectedCmd {
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
