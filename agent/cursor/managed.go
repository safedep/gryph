package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// managedHooksPath is the enterprise hooks.json of the platform (Cursor
// hooks reference, 2026-10-03). Enterprise entries take priority over the
// team, project and user entries, and any deny wins. The reference does
// not say that a user cannot turn them off.
func managedHooksPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Cursor/hooks.json"
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "Cursor", "hooks.json")
	default:
		return "/etc/cursor/hooks.json"
	}
}

// ManagedHookPath implements agent.ManagedInstaller.
func (a *Adapter) ManagedHookPath() string { return managedHooksPath() }

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
// programs. Every Gryph entry sets failClosed, so a crash or a timeout of
// the hook blocks the action instead of letting it through. Cursor has no
// lock, so opts.Lock has no effect.
func installManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	if opts.Command == "" || !filepath.IsAbs(opts.Command) {
		return nil, fmt.Errorf("the managed hook command needs an absolute path, got %q", opts.Command)
	}
	return agent.UpdateManagedJSON(path, opts, func(doc map[string]any) error {
		doc["version"] = 1
		agent.ReplaceGryphEntries(agent.SubTable(doc, "hooks"), HookTypes, agent.IsGryphCommandEntry, func(hookType string) map[string]any {
			return map[string]any{
				"command":    utils.HookCommand(opts.Command, "cursor", hookType),
				"failClosed": true,
			}
		}, false)
		return nil
	})
}

// uninstallManagedAt removes the Gryph entries. The entries of other
// programs stay.
func uninstallManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return agent.UpdateManagedJSON(path, opts, func(doc map[string]any) error {
		if hooks, ok := doc["hooks"].(map[string]any); ok {
			agent.ReplaceGryphEntries(hooks, HookTypes, agent.IsGryphCommandEntry, nil, true)
		}
		return nil
	})
}
