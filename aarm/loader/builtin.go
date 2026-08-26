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

// Built-in rule IDs.
const (
	builtinProtectedFilesRuleID    = BuiltinRuleIDPrefix + "protected-files"
	builtinProtectedCommandsRuleID = BuiltinRuleIDPrefix + "protected-commands"
)

// builtinCommandPatterns is the best-effort command_exec guard. It matches
// common mutation commands that reference Gryph's own control surfaces by
// name. Shell obfuscation (eval, variable indirection, base64) can evade it;
// the file_write / file_delete rule is the real boundary because it matches
// the normalized action target rather than a command string.
var builtinCommandPatterns = []string{
	`(^|[\s;&|])(rm|mv|cp|tee|truncate|chmod|chown)\s+[^\n]*/gryph/(keys/|[^\s]*\.db|[^\s]*receipt)`,
	`(^|[\s;&|])(rm|mv|cp|tee|truncate)\s+[^\n]*\.(claude/settings|cursor/hooks|codex/hooks|codeium/windsurf/hooks)\.json`,
}

// BuiltinSource emits the embedded self-protection rules. It is constructed by
// the runtime with the resolved set of file globs to protect (policy source
// paths, the Gryph config / data / key paths, and the agents' hook-config
// paths). The Loader recognizes it by type so its rules bypass the reserved-ID
// check and the disabled: filter.
type BuiltinSource struct {
	// FileGlobs are doublestar glob patterns (forward-slash normalized) for
	// every path a write or delete must be blocked against. Empty disables the
	// file rule entirely rather than matching every path.
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
	rules := make([]pdp.Rule, 0, 2)

	if len(s.FileGlobs) > 0 {
		rules = append(rules, pdp.Rule{
			ID:          builtinProtectedFilesRuleID,
			Description: "Block agent writes and deletes to Gryph's own policy files, config, database, signing keys, and the agents' hook configs.",
			Action:      model.DecisionBlock,
			Severity:    model.SeverityCritical,
			Tags:        []string{"self-protection", "builtin"},
			Message:     "Blocked by Gryph self-protection: {{.Action.Params.Path}} is a protected Gryph control file.",
			Match: pdp.Match{
				ActionTypes:  []string{string(model.ActionFileWrite), string(model.ActionFileDelete)},
				FilePatterns: append([]string(nil), s.FileGlobs...),
			},
		})
	}

	rules = append(rules, pdp.Rule{
		ID:          builtinProtectedCommandsRuleID,
		Description: "Best-effort block of shell commands that mutate Gryph control files. Shell obfuscation can evade this; the file rule is the real boundary.",
		Action:      model.DecisionBlock,
		Severity:    model.SeverityHigh,
		Tags:        []string{"self-protection", "builtin"},
		Message:     "Blocked by Gryph self-protection: command appears to modify a protected Gryph control file.",
		Match: pdp.Match{
			ActionTypes:     []string{string(model.ActionCommandExec)},
			CommandPatterns: append([]string(nil), builtinCommandPatterns...),
		},
	})

	return []*pdp.Policy{{Rules: rules}}, nil
}
