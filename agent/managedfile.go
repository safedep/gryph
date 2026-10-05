package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/platform/nofollow"
)

// UpdateManagedJSON edits the JSON document at a managed path. A missing
// file is an empty document. A file that does not parse is refused, because
// a rewrite would drop the administrator's settings. The write goes through
// a temporary file and a rename, refuses a link at path, and is skipped
// when the document did not change, so a repeated run is safe.
func UpdateManagedJSON(path string, opts ManagedInstallOptions, edit func(doc map[string]any) error) (*ManagedInstallResult, error) {
	existing, err := nofollow.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	doc := map[string]any{}
	if existing != nil {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("%s does not parse, so Gryph leaves it: %w", path, err)
		}
	}
	if err := edit(doc); err != nil {
		return nil, err
	}
	result := &ManagedInstallResult{Path: path}
	if existing == nil && len(doc) == 0 {
		return result, nil
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
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

// RemoveManagedFile deletes the managed file that Gryph owns in full. It
// refuses a link at path.
func RemoveManagedFile(path string, opts ManagedInstallOptions) (*ManagedInstallResult, error) {
	result := &ManagedInstallResult{Path: path}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return result, nil
	case err != nil:
		return nil, err
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, fmt.Errorf("%s is a symbolic link, not the Gryph file", path)
	}
	result.Changed = true
	if opts.DryRun {
		return result, nil
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	return result, nil
}

// SubTable returns the object at key in doc, and creates it when missing
// or of another type.
func SubTable(doc map[string]any, key string) map[string]any {
	if t, ok := doc[key].(map[string]any); ok {
		return t
	}
	t := map[string]any{}
	doc[key] = t
	return t
}

// EntryList returns the entries of one hook event as a list of objects,
// whatever the decoder produced.
func EntryList(v any) []map[string]any {
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

// IsGryphCommandEntry reports whether an entry of the form {command: ...}
// runs a Gryph hook.
func IsGryphCommandEntry(entry map[string]any) bool {
	command, _ := entry["command"].(string)
	return utils.IsGryphCommand(command)
}

// IsGryphMatcherEntry reports whether an entry of the form
// {matcher: ..., hooks: [{command: ...}]} runs only Gryph hooks. An entry
// that also runs another program is not Gryph's.
func IsGryphMatcherEntry(entry map[string]any) bool {
	commands := EntryList(entry["hooks"])
	if len(commands) == 0 {
		return false
	}
	for _, cmd := range commands {
		if !IsGryphCommandEntry(cmd) {
			return false
		}
	}
	return true
}

// WithoutGryphEntries drops the entries that isGryph accepts.
func WithoutGryphEntries(entries []map[string]any, isGryph func(map[string]any) bool) []map[string]any {
	var out []map[string]any
	for _, entry := range entries {
		if !isGryph(entry) {
			out = append(out, entry)
		}
	}
	return out
}

// ReplaceGryphEntries puts one Gryph entry per hook type into the hooks
// table, after the entries of other programs, and drops the Gryph entries
// an earlier run wrote. With remove, it writes no new entry and drops an
// event that holds nothing else.
func ReplaceGryphEntries(hooks map[string]any, hookTypes []string, isGryph func(map[string]any) bool, entry func(hookType string) map[string]any, remove bool) {
	for _, hookType := range hookTypes {
		entries := WithoutGryphEntries(EntryList(hooks[hookType]), isGryph)
		if remove {
			if len(entries) == 0 {
				delete(hooks, hookType)
			} else {
				hooks[hookType] = entries
			}
			continue
		}
		hooks[hookType] = append(entries, entry(hookType))
	}
}
