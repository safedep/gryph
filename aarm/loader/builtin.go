package loader

import (
	"context"
	"path/filepath"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
)

// BuiltinRuleIDPrefix is the reserved ID prefix for built-in self-protection
// rules. User policy documents must not declare a rule with this prefix; the
// Loader rejects any that do. Built-in rules are never subject to a user
// document's disabled: list, so a repo-local policy cannot weaken them.
const BuiltinRuleIDPrefix = "gryph-builtin-"

const builtinProtectedFilesRuleID = BuiltinRuleIDPrefix + "protected-files"

// BuiltinSource emits the embedded self-protection rules. It is constructed by
// the runtime with the resolved set of file globs to protect (policy source
// paths, the Gryph config / data / key paths, and the agents' hook-config
// paths). The Loader recognizes it by type so its rules bypass the reserved-ID
// check and the disabled: filter.
type BuiltinSource struct {
	// FileGlobs are doublestar glob patterns (forward-slash normalized) for
	// every path that an agent write, delete, or shell command must not
	// change. Empty disables the rule rather than matching every path.
	FileGlobs []string
}

// NewBuiltinSource builds a BuiltinSource from the given protected paths. Each
// path is forward-slash normalized; duplicates and empties are dropped. Pass
// glob patterns directly (e.g. "**/.cursor/hooks.json") or absolute paths.
func NewBuiltinSource(globs ...string) *BuiltinSource {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(globs))
	for _, g := range globs {
		g = filepath.ToSlash(g)
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return &BuiltinSource{FileGlobs: out}
}

func (s *BuiltinSource) Name() string { return "builtin" }

// Load returns the built-in self-protection policy. The rules are constructed
// in Go (rather than embedded YAML) because their file patterns are resolved
// at runtime from the operator's config and the installed agents.
func (s *BuiltinSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	if len(s.FileGlobs) == 0 {
		return []*pdp.Policy{{}}, nil
	}
	rule := pdp.Rule{
		ID:          builtinProtectedFilesRuleID,
		Description: "Block agent writes, deletes, and shell commands that change Gryph's own policy files, config, database, signing keys, or the agents' hook configs. The shell check is best effort.",
		Action:      model.DecisionBlock,
		Severity:    model.SeverityCritical,
		Tags:        []string{"self-protection", "builtin"},
		Message:     "Blocked by Gryph self-protection: {{if .Action.Params.Path}}{{.Action.Params.Path}} is a protected Gryph control file.{{else}}the command changes a protected Gryph control file.{{end}}",
		Match: pdp.Match{
			ActionTypes:  []string{string(model.ActionFileWrite), string(model.ActionFileDelete), string(model.ActionCommandExec)},
			FilePatterns: append([]string(nil), s.FileGlobs...),
		},
	}
	return []*pdp.Policy{{Rules: []pdp.Rule{rule}}}, nil
}
