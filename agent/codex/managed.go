package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/platform/nofollow"
)

const lockKey = "allow_managed_hooks_only"

// requirementsPath is the Codex requirements file of the platform (Codex
// docs, 2026-10-03). Managed hooks live inline in it under [hooks].
func requirementsPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "OpenAI", "Codex", "requirements.toml")
	}
	return "/etc/codex/requirements.toml"
}

// ManagedHookPath implements agent.ManagedInstaller.
func (a *Adapter) ManagedHookPath() string { return requirementsPath() }

// ManagedClass implements agent.ManagedInstaller. Codex marks managed hooks
// as trusted, and the user cannot disable them (Codex docs, 2026-10-03).
func (a *Adapter) ManagedClass() agent.ManagedClass { return agent.ManagedClassLocked }

// ManagedLockSwitch implements agent.ManagedLockSwitcher: the requirements
// file can set allow_managed_hooks_only.
func (a *Adapter) ManagedLockSwitch() bool { return true }

// InstallManaged implements agent.ManagedInstaller.
func (a *Adapter) InstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return installManagedAt(a.ManagedHookPath(), opts)
}

// UninstallManaged implements agent.ManagedInstaller.
func (a *Adapter) UninstallManaged(_ context.Context, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	return uninstallManagedAt(a.ManagedHookPath(), opts)
}

// installManagedAt rewrites requirements.toml with the Gryph entries. It
// keeps every other key, pins [features] hooks = true so a user cannot turn
// hooks off, and sets the lock when asked. Comments do not survive the
// rewrite, because the file goes through a parse and a marshal.
func installManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	if opts.Command == "" || !filepath.IsAbs(opts.Command) {
		return nil, fmt.Errorf("the managed hook command needs an absolute path, got %q", opts.Command)
	}
	doc, existing, err := readRequirements(path)
	if err != nil {
		return nil, err
	}
	agent.SubTable(doc, "features")["hooks"] = true
	if opts.Lock {
		doc[lockKey] = true
	} else {
		delete(doc, lockKey)
	}
	agent.ReplaceGryphEntries(agent.SubTable(doc, "hooks"), HookTypes, agent.IsGryphMatcherEntry, func(hookType string) map[string]any {
		return gryphEntry(opts.Command, hookType)
	}, false)
	return writeRequirements(path, doc, existing, opts)
}

// uninstallManagedAt removes the Gryph entries and the lock. The hooks
// feature stays pinned, because other managed hooks may need it.
func uninstallManagedAt(path string, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	doc, existing, err := readRequirements(path)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return &agent.ManagedInstallResult{Path: path}, nil
	}
	delete(doc, lockKey)
	if hooks, ok := doc["hooks"].(map[string]any); ok {
		agent.ReplaceGryphEntries(hooks, HookTypes, agent.IsGryphMatcherEntry, nil, true)
		if len(hooks) == 0 {
			delete(doc, "hooks")
		}
	}
	return writeRequirements(path, doc, existing, opts)
}

// readRequirements parses the file. A missing file is an empty document.
// A file that does not parse is refused, because a rewrite would drop the
// administrator's settings. The decode always goes into a fresh map: go-toml
// panics on a map that already holds a slice.
func readRequirements(path string) (map[string]any, []byte, error) {
	existing, err := nofollow.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	doc := map[string]any{}
	if err := toml.Unmarshal(existing, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s does not parse, so Gryph leaves it: %w", path, err)
	}
	return doc, existing, nil
}

func writeRequirements(path string, doc map[string]any, existing []byte, opts agent.ManagedInstallOptions) (*agent.ManagedInstallResult, error) {
	data, err := toml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	result := &agent.ManagedInstallResult{Path: path, Locked: doc[lockKey] == true}
	if bytes.Equal(existing, data) {
		return result, nil
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

func gryphEntry(program, hookType string) map[string]any {
	entry := map[string]any{
		"hooks": []map[string]any{{
			"type":    "command",
			"command": utils.HookCommand(program, "codex", hookType),
			"timeout": int64(hookTimeout / time.Second),
		}},
	}
	if m := hookMatcher(hookType); m != "" {
		entry["matcher"] = m
	}
	return entry
}
