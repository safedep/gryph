package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/aarm/model"
)

// ActionDigest is the identity of an action for an approval request and a
// grant. It covers the normalized action and its working directory, so
// the same command in another directory is another action, and a grant
// for one never matches the other. The digest never covers the content
// of a write, because a grant is for an action, not for a payload.
func ActionDigest(a *model.Action) string {
	if a == nil {
		return ""
	}
	fields := map[string]any{
		"type":        string(a.Type),
		"tool":        a.Tool,
		"operation":   a.Operation,
		"agent":       a.Agent,
		"working_dir": cleanDir(a.WorkingDir),
		"path":        a.Parameters.Path,
		"command":     a.Parameters.Command,
		"args":        a.Parameters.Args,
		"url":         a.Parameters.URL,
	}
	data, err := canonical.MarshalJSON(fields)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// cleanDir returns dir as one canonical path. The service cannot resolve
// the links of a directory it must not read, so the clean path is the
// identity.
func cleanDir(dir string) string {
	if dir == "" {
		return ""
	}
	if !filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Clean(dir)
}

// Summary is the one line of an action that an approver reads: the type
// and the one parameter that names what the action does.
func Summary(a *model.Action) string {
	if a == nil {
		return ""
	}
	switch {
	case a.Parameters.Command != "":
		return string(a.Type) + ": " + a.Parameters.Command
	case a.Parameters.URL != "":
		return string(a.Type) + ": " + a.Parameters.URL
	case a.Parameters.Path != "":
		return string(a.Type) + ": " + a.Parameters.Path
	case a.Tool != "":
		return string(a.Type) + ": " + a.Tool
	}
	return string(a.Type)
}
