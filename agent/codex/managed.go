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
	features := table(doc, "features")
	features["hooks"] = true
	if opts.Lock {
		doc[lockKey] = true
	} else {
		delete(doc, lockKey)
	}
	hooks := table(doc, "hooks")
	for _, hookType := range HookTypes {
		entries := withoutGryph(entryList(hooks[hookType]))
		hooks[hookType] = append(entries, gryphEntry(opts.Command, hookType))
	}
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
		for _, hookType := range HookTypes {
			entries := withoutGryph(entryList(hooks[hookType]))
			if len(entries) == 0 {
				delete(hooks, hookType)
			} else {
				hooks[hookType] = entries
			}
		}
		if len(hooks) == 0 {
			delete(doc, "hooks")
		}
	}
	return writeRequirements(path, doc, existing, opts)
}

// readRequirements parses the file. A missing file is an empty document.
// A file that does not parse is refused, because a rewrite would drop the
// administrator's settings.
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

// table returns the sub-table name of doc, and creates it when missing.
func table(doc map[string]any, name string) map[string]any {
	if t, ok := doc[name].(map[string]any); ok {
		return t
	}
	t := map[string]any{}
	doc[name] = t
	return t
}

// entryList returns the matcher entries of one event as a list of tables.
func entryList(v any) []map[string]any {
	var out []map[string]any
	switch list := v.(type) {
	case []any:
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = list
	}
	return out
}

// withoutGryph drops the entries whose every command is a Gryph hook
// command. An entry with a command of another program stays.
func withoutGryph(entries []map[string]any) []map[string]any {
	var out []map[string]any
	for _, entry := range entries {
		if !isGryphEntry(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func isGryphEntry(entry map[string]any) bool {
	commands := entryList(entry["hooks"])
	if len(commands) == 0 {
		return false
	}
	for _, cmd := range commands {
		command, _ := cmd["command"].(string)
		if !utils.IsGryphCommand(command) {
			return false
		}
	}
	return true
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
