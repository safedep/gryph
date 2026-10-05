package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/platform/nofollow"
)

// managedDropIn is the file Gryph owns under managed-settings.d. Claude Code
// merges the drop-ins after managed-settings.json in alphabetical order,
// so the administrator's own file stays untouched (managed settings docs,
// 2026-10-03).
const managedDropIn = "50-gryph.json"

// managedSettingsDir is the Claude Code managed settings directory of the
// platform (managed settings docs, 2026-10-03). Claude Code does not read
// the legacy Windows path under ProgramData.
func managedSettingsDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}

// ManagedHookPath implements agent.ManagedInstaller.
func (a *Adapter) ManagedHookPath() string {
	return filepath.Join(managedSettingsDir(), "managed-settings.d", managedDropIn)
}

// ManagedClass implements agent.ManagedInstaller. A user or project
// disableAllHooks cannot turn off a managed hook, and allowManagedHooksOnly
// locks out the other hooks (managed settings docs, 2026-10-03).
func (a *Adapter) ManagedClass() agent.ManagedClass { return agent.ManagedClassLocked }

// ManagedLockSwitch implements agent.ManagedLockSwitcher: the drop-in can
// set allowManagedHooksOnly.
func (a *Adapter) ManagedLockSwitch() bool { return true }

// InstallManaged implements agent.ManagedInstaller.
func (a *Adapter) InstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return installManagedAt(a.ManagedHookPath(), opts)
}

// UninstallManaged implements agent.ManagedInstaller.
func (a *Adapter) UninstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return uninstallManagedAt(a.ManagedHookPath(), opts)
}

// managedDocument renders the drop-in: the Gryph hooks and, with lock,
// allowManagedHooksOnly. A user disableAllHooks cannot turn managed hooks
// off, so the lock is not needed for the hooks to hold. It only stops the
// developer's own hooks.
func managedDocument(command string, lock bool) ([]byte, error) {
	doc := map[string]any{"hooks": GenerateHooksConfig(command)}
	if lock {
		doc["allowManagedHooksOnly"] = true
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func installManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	if opts.Command == "" || !filepath.IsAbs(opts.Command) {
		return nil, fmt.Errorf("the managed hook command needs an absolute path, got %q", opts.Command)
	}
	data, err := managedDocument(opts.Command, opts.Lock)
	if err != nil {
		return nil, err
	}
	result := &agent.ManagedInstallResult{Path: path, Locked: opts.Lock}
	existing, err := nofollow.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(existing, data):
		return result, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	result.Changed = true
	if opts.DryRun {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := nofollow.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

func uninstallManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return agent.RemoveManagedFile(path, opts)
}
