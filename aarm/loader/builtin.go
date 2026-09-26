package loader

import (
	"context"
	"path/filepath"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/shellcmd"
)

// BuiltinRuleIDPrefix is the reserved ID prefix for built-in self-protection
// rules. User policy documents must not declare a rule with this prefix; the
// Loader rejects any that do. Built-in rules are never subject to a user
// document's disabled: list, so a repo-local policy cannot weaken them.
const BuiltinRuleIDPrefix = "gryph-builtin-"

const (
	builtinProtectedFilesRuleID = BuiltinRuleIDPrefix + "protected-files"
	builtinProtectedReadsRuleID = BuiltinRuleIDPrefix + "protected-reads"
	builtinHookCommandRuleID    = BuiltinRuleIDPrefix + "hook-command"
)

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

	// ReadGlobs are the paths that an agent read or shell command must not
	// read. Empty omits the read rule.
	ReadGlobs []string
}

// NewBuiltinSource builds a BuiltinSource from the given protected paths. Each
// path is forward-slash normalized; duplicates and empties are dropped. Pass
// glob patterns directly (e.g. "**/.cursor/hooks.json") or absolute paths.
func NewBuiltinSource(globs ...string) *BuiltinSource {
	return &BuiltinSource{FileGlobs: normalizeGlobs(globs)}
}

// WithReadGlobs sets the paths that the read rule protects.
func (s *BuiltinSource) WithReadGlobs(globs ...string) *BuiltinSource {
	s.ReadGlobs = normalizeGlobs(globs)
	return s
}

func normalizeGlobs(globs []string) []string {
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
	return out
}

func (s *BuiltinSource) Name() string { return "builtin" }

// Load returns the built-in self-protection policy. The rules are constructed
// in Go (rather than embedded YAML) because their file patterns are resolved
// at runtime from the operator's config and the installed agents.
func (s *BuiltinSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	var rules []pdp.Rule
	if len(s.FileGlobs) > 0 {
		rules = append(rules, s.protectedFilesRule())
	}
	if len(s.ReadGlobs) > 0 {
		rules = append(rules, s.protectedReadsRule())
	}
	rules = append(rules, hookCommandRule())
	return []*pdp.Policy{{Rules: rules}}, nil
}

func (s *BuiltinSource) protectedFilesRule() pdp.Rule {
	return pdp.Rule{
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
}

func (s *BuiltinSource) protectedReadsRule() pdp.Rule {
	return pdp.Rule{
		ID:          builtinProtectedReadsRuleID,
		Description: "Block agent reads and shell commands that read Gryph's database or receipt signing key. The shell check is best effort.",
		Action:      model.DecisionBlock,
		Severity:    model.SeverityCritical,
		Tags:        []string{"self-protection", "builtin"},
		Message:     "Blocked by Gryph self-protection: {{if .Action.Params.Path}}a read of {{.Action.Params.Path}} reads a protected Gryph file.{{else}}the command reads a protected Gryph file.{{end}}",
		Match: pdp.Match{
			ActionTypes:  []string{string(model.ActionFileRead), string(model.ActionCommandExec)},
			FilePatterns: append([]string(nil), s.ReadGlobs...),
			FileAccess:   []string{string(shellcmd.AccessRead)},
		},
	}
}

// hookCommandMessage covers a call of gryph, or of a program that the shell
// check cannot resolve, with the literal argument "_hook".
const hookCommandMessage = "Blocked by Gryph self-protection: the command runs gryph _hook. Only an agent hook may run it."

// hookCommandRule blocks an agent shell command that runs "gryph _hook". The
// command can record a forged event, such as a user prompt that resets
// context.actions_since_intent.
func hookCommandRule() pdp.Rule {
	return pdp.Rule{
		ID:          builtinHookCommandRuleID,
		Description: "Block agent shell commands that run the Gryph hook entry point, gryph _hook. The shell check is best effort.",
		Action:      model.DecisionBlock,
		Severity:    model.SeverityCritical,
		Tags:        []string{"self-protection", "builtin"},
		Message:     hookCommandMessage,
		Match:       pdp.Match{ActionTypes: []string{string(model.ActionCommandExec)}},
		Condition:   "action.gryph_hook == true",
	}
}
