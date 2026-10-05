package windsurf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

// managedHooksPath is the system hooks.json of the platform (Cascade hooks
// reference, 2026-10-03). The reference says a user cannot disable it
// without root, and that the system, user and workspace entries all run.
// The legacy Windsurf path is read only when this file is absent.
func managedHooksPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Devin/hooks.json"
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "Devin", "hooks.json")
	default:
		return "/etc/devin/hooks.json"
	}
}

// ManagedHookPath implements agent.ManagedInstaller.
func (a *Adapter) ManagedHookPath() string { return managedHooksPath() }

// ManagedClass implements agent.ManagedInstaller. The reference documents
// no lock against a user entry that answers first, so the class stays at
// the system path.
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
// programs. Windsurf has no lock, so opts.Lock has no effect.
func installManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	if opts.Command == "" || !filepath.IsAbs(opts.Command) {
		return nil, fmt.Errorf("the managed hook command needs an absolute path, got %q", opts.Command)
	}
	return agent.UpdateManagedJSON(path, opts, func(doc map[string]any) error {
		agent.ReplaceGryphEntries(agent.SubTable(doc, "hooks"), HookTypes, agent.IsGryphCommandEntry, func(hookType string) map[string]any {
			return map[string]any{"command": utils.HookCommand(opts.Command, "windsurf", hookType)}
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
