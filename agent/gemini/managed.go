package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// managedSettingsPath is the system settings.json of the platform (Gemini
// CLI settings source, 2026-10-03). The system file wins on a conflict and
// its hook lists concatenate with the user's, so Gryph pins
// hooksConfig.enabled there. The source does not say that a user cannot
// list the Gryph command under hooksConfig.disabled.
func managedSettingsPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/GeminiCli/settings.json"
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "gemini-cli", "settings.json")
	default:
		return "/etc/gemini-cli/settings.json"
	}
}

// ManagedHookPath implements agent.ManagedInstaller.
func (a *Adapter) ManagedHookPath() string { return managedSettingsPath() }

// ManagedClass implements agent.ManagedInstaller.
func (a *Adapter) ManagedClass() agent.ManagedClass { return agent.ManagedClassSystemPath }

// InstallManaged implements agent.ManagedInstaller.
func (a *Adapter) InstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return installManagedAt(a.ManagedHookPath(), opts)
}

// UninstallManaged implements agent.ManagedInstaller.
func (a *Adapter) UninstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return uninstallManagedAt(a.ManagedHookPath(), opts)
}

// installManagedAt writes the Gryph entries after the entries of other
// programs and pins hooksConfig.enabled. Gemini CLI has no lock, so
// opts.Lock has no effect.
func installManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	if opts.Command == "" || !filepath.IsAbs(opts.Command) {
		return nil, fmt.Errorf("the managed hook command needs an absolute path, got %q", opts.Command)
	}
	return agent.UpdateManagedJSON(path, opts, func(doc map[string]any) error {
		agent.SubTable(doc, "hooksConfig")["enabled"] = true
		agent.ReplaceGryphEntries(agent.SubTable(doc, "hooks"), HookTypes, agent.IsGryphMatcherEntry, func(hookType string) map[string]any {
			entry := map[string]any{
				"hooks": []map[string]any{{
					"type":    "command",
					"command": utils.HookCommand(opts.Command, "gemini", hookType),
				}},
			}
			if hookType == "BeforeTool" || hookType == "AfterTool" {
				entry["matcher"] = "*"
			}
			return entry
		}, false)
		return nil
	})
}

// uninstallManagedAt removes the Gryph entries. The other settings stay,
// hooksConfig.enabled included, because other hooks may need it.
func uninstallManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return agent.UpdateManagedJSON(path, opts, func(doc map[string]any) error {
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			return nil
		}
		agent.ReplaceGryphEntries(hooks, HookTypes, agent.IsGryphMatcherEntry, nil, true)
		if len(hooks) == 0 {
			delete(doc, "hooks")
		}
		return nil
	})
}
