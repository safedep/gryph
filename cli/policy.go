package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/safedep/dry/log"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/schema"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func NewPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage Gryph security policies",
		Long: "Author, inspect, and operate Gryph's security policy layer. " +
			"Subcommands cover scaffolding (init), validation, dry-run testing, " +
			"and as the AARM implementation grows, receipts, context, and " +
			"approval workflows.",
	}

	cmd.AddCommand(
		newPolicyInitCmd(),
		newPolicySchemaCmd(),
		newPolicySourcesCmd(),
		newPolicyValidateCmd(),
		newPolicyTestCmd(),
	)

	return cmd
}

func policyColorizer(app *App) *tui.Colorizer {
	if app == nil || app.Config == nil {
		return tui.NewColorizer(false)
	}
	return tui.NewColorizer(app.Config.ShouldUseColors())
}

func newPolicyInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init <path>",
		Short: "Write a fully documented example policy to the given path",
		Long: "Writes the embedded, fully commented example policy to <path>. " +
			"Use this as the starting point for authoring your own rules: every " +
			"feature of the policy language is demonstrated with inline " +
			"documentation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			if !force {
				if _, err := os.Stat(target); err == nil {
					return ErrConfig("refusing to overwrite existing file (use --force to replace)", fmt.Errorf("%s exists", target))
				} else if !os.IsNotExist(err) {
					return ErrConfig("stat target path", err)
				}
			}

			if dir := filepath.Dir(target); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return ErrConfig("create parent directory", err)
				}
			}

			if err := os.WriteFile(target, []byte(pdp.ExampleYAML()), 0o644); err != nil {
				return ErrConfig("write policy file", err)
			}

			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy init, falling back to plain output: %v", err)
			}
			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s Wrote example policy to %s\n", c.StatusOK(), c.Path(target))
			_, _ = fmt.Fprintf(out, "  %s\n", c.Dim(fmt.Sprintf("Next: edit, then `gryph policy validate --file %s`", target)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the target path if it already exists")
	return cmd
}

func newPolicySchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for Gryph policy documents",
		Long: "Prints the JSON Schema (draft 2020-12) that describes Gryph policy YAML/JSON. " +
			"Pipe into editor tooling, validators, or pass to an AI agent so it can author " +
			"valid policies without guessing the field set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), schema.PolicyJSON())
			return err
		},
	}
}

func newPolicySourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List the policy sources Gryph will load, in order",
		Long: "Resolves the configured policy sources without loading them. Useful for " +
			"answering \"where is Gryph looking for policy?\" before any files exist.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			ldr, err := buildPolicyLoader(app.Config, app.Paths, "")
			if err != nil {
				return ErrConfig("failed to assemble policy loader", err)
			}

			renderSources(cmd.OutOrStdout(), policyColorizer(app), ldr.Sources(), globalFlags.Verbose)
			return nil
		},
	}
}

type sourceRow struct {
	Kind     string
	Path     string
	Optional bool
	Status   string
	Problem  bool
	Hints    []string
}

const (
	sourceStatusFound           = "found"
	sourceStatusMissing         = "missing"
	sourceStatusUnreadable      = "unreadable"
	sourceStatusNotFound        = "not found"
	sourceStatusEmpty           = "empty"
	sourceStatusUnknown         = "unknown"
	sourceStatusIsDirectory     = "is a directory"
	sourceStatusInvalidStartDir = "invalid start dir"
)

func sourceRows(sources []loader.Source) []sourceRow {
	rows := make([]sourceRow, 0, len(sources))
	for _, src := range sources {
		rows = append(rows, sourceToRow(src))
	}
	return rows
}

func sourceToRow(src loader.Source) sourceRow {
	switch s := src.(type) {
	case *loader.FileSource:
		status, problem := fileSourceStatus(s.Path, s.Optional)
		return sourceRow{Kind: "file", Path: s.Path, Optional: s.Optional, Status: status, Problem: problem}
	case *loader.DirSource:
		status, problem := dirSourceStatus(s.Path, s.Optional)
		return sourceRow{
			Kind:     "dir",
			Path:     s.Path,
			Optional: s.Optional,
			Status:   status,
			Problem:  problem,
			Hints:    []string{"Loads every *.yaml or *.yml file sorted by filename"},
		}
	case *loader.ConventionalSource:
		filenames := s.Filenames
		if len(filenames) == 0 {
			filenames = loader.DefaultConventionalFilenames
		}
		hints := []string{
			"Starts from " + s.StartDir,
			"Walks up from this directory until a policy file is found",
			"Looks for " + strings.Join(filenames, " or "),
		}
		if s.StopAt != "" {
			hints = append(hints, "Stops at "+s.StopAt)
		}
		status, path, problem := conventionalSourceStatus(s)
		if path == "" {
			path = s.StartDir
		}
		return sourceRow{Kind: "conventional", Path: path, Optional: true, Status: status, Problem: problem, Hints: hints}
	default:
		return sourceRow{Kind: "source", Path: src.Name(), Status: sourceStatusUnknown}
	}
}

func fileSourceStatus(path string, optional bool) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return missingStatus(err, optional)
	}
	if info.IsDir() {
		return sourceStatusIsDirectory, true
	}
	return sourceStatusFound, false
}

func dirSourceStatus(path string, optional bool) (string, bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return missingStatus(err, optional)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".yaml" || ext == ".yml" {
			count++
		}
	}
	if count == 0 {
		return sourceStatusEmpty, false
	}
	if count == 1 {
		return "1 policy file", false
	}
	return fmt.Sprintf("%d policy files", count), false
}

func conventionalSourceStatus(s *loader.ConventionalSource) (string, string, bool) {
	if s.StartDir == "" {
		return sourceStatusNotFound, "", false
	}
	filenames := s.Filenames
	if len(filenames) == 0 {
		filenames = loader.DefaultConventionalFilenames
	}

	dir, err := filepath.Abs(s.StartDir)
	if err != nil {
		return sourceStatusInvalidStartDir, "", true
	}
	stopAt := s.StopAt
	if stopAt != "" {
		if abs, err := filepath.Abs(stopAt); err == nil {
			stopAt = abs
		}
	}

	for {
		for _, name := range filenames {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return sourceStatusUnreadable, candidate, true
			}
			if !info.IsDir() {
				return sourceStatusFound, candidate, false
			}
		}

		if stopAt != "" && dir == stopAt {
			return sourceStatusNotFound, "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return sourceStatusNotFound, "", false
		}
		dir = parent
	}
}

func missingStatus(err error, optional bool) (string, bool) {
	if errors.Is(err, fs.ErrNotExist) {
		if optional {
			return sourceStatusMissing, false
		}
		return sourceStatusMissing, true
	}
	return sourceStatusUnreadable, true
}

func renderSources(w io.Writer, c *tui.Colorizer, sources []loader.Source, verbose bool) {
	if len(sources) == 0 {
		_, _ = fmt.Fprintln(w, c.Warning("No policy sources configured."))
		return
	}

	rows := sourceRows(sources)
	kindWidth := 0
	for _, r := range rows {
		if len(r.Kind) > kindWidth {
			kindWidth = len(r.Kind)
		}
	}

	_, _ = fmt.Fprintln(w, c.Header("Policy sources"))
	for i, r := range rows {
		idx := c.Number(fmt.Sprintf("%d", i+1))
		kind := c.Cyan(fmt.Sprintf("%-*s", kindWidth, r.Kind))
		path := c.Path(r.Path)
		status := decorateSourceStatus(c, r.Status, r.Problem)
		line := fmt.Sprintf("%s  %s  %s  %s", idx, kind, path, status)
		if r.Optional {
			line += "  " + c.Dim("optional")
		}
		_, _ = fmt.Fprintln(w, line)
		if verbose {
			for _, h := range r.Hints {
				_, _ = fmt.Fprintf(w, "   %s\n", c.Dim(h))
			}
		}
	}
}

func decorateSourceStatus(c *tui.Colorizer, status string, problem bool) string {
	if problem {
		return c.Warning(status)
	}
	switch {
	case status == sourceStatusFound, strings.HasPrefix(status, sourceStatusFound+": "), strings.Contains(status, "policy file"):
		return c.Success(status)
	case status == sourceStatusMissing || status == sourceStatusNotFound || status == sourceStatusEmpty || status == sourceStatusUnknown:
		return c.Dim(status)
	default:
		return status
	}
}

func newPolicyValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configured policy sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			ldr, err := buildPolicyLoader(app.Config, app.Paths, file)
			if err != nil {
				return ErrConfig("failed to assemble policy loader", err)
			}

			policy, err := ldr.Load(cmd.Context())
			if err != nil {
				return ErrConfig("failed to validate policy", err)
			}

			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s %s\n",
				c.StatusOK(),
				c.Success(fmt.Sprintf("Policy valid: %d rules from %d source(s)", len(policy.Rules), len(ldr.Sources()))))
			_, _ = fmt.Fprintln(out)
			renderSources(out, c, ldr.Sources(), false)
			if len(policy.Disabled) > 0 {
				_, _ = fmt.Fprintf(out, "\n%s %s\n", c.Header("Disabled rule IDs:"), c.Dim(strings.Join(policy.Disabled, ", ")))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "explicit policy file (overrides config sources)")
	return cmd
}

func newPolicyTestCmd() *cobra.Command {
	var (
		file         string
		format       string
		actionType   string
		tool         string
		path         string
		command      string
		url          string
		agentName    string
		workingDir   string
		project      string
		totalActions int
		filesRead    int
		filesWritten int
		commandsExec int
		errorsCount  int
	)

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Dry-run a synthetic action through the merged policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			ldr, err := buildPolicyLoader(app.Config, app.Paths, file)
			if err != nil {
				return ErrConfig("failed to assemble policy loader", err)
			}

			policy, err := ldr.Load(cmd.Context())
			if err != nil {
				return ErrConfig("failed to load policy", err)
			}
			engine, err := pdp.New(policy)
			if err != nil {
				return ErrConfig("failed to compile policy", err)
			}

			at := model.ActionType(actionType)
			if at == "" {
				at = model.ActionToolUse
			}

			action := &aarmsec.Action{
				Type:       at,
				Tool:       tool,
				Agent:      agentName,
				WorkingDir: workingDir,
				Project:    project,
				Parameters: aarmsec.Parameters{
					Path:    path,
					Command: command,
					URL:     url,
				},
			}
			snapshot := &aarmsec.ContextSnapshot{
				TotalActions:     totalActions,
				FilesRead:        filesRead,
				FilesWritten:     filesWritten,
				CommandsExecuted: commandsExec,
				Errors:           errorsCount,
			}

			result, err := engine.Evaluate(context.Background(), action, snapshot)
			if err != nil {
				return ErrConfig("failed to evaluate policy", err)
			}

			view := policyTestView{
				Sources:        sourceNames(ldr.Sources()),
				Action:         actionSummary(action),
				Decision:       string(result.Decision),
				MatchedRuleIDs: result.MatchedRuleIDs,
				Message:        result.Message,
				Severity:       string(result.Severity),
				Tags:           result.Tags,
			}

			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(view)
			}

			renderPolicyTest(cmd.OutOrStdout(), policyColorizer(app), view, ldr.Sources())
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "explicit policy file (overrides config sources)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().StringVar(&actionType, "action", string(model.ActionToolUse), "action type")
	cmd.Flags().StringVar(&tool, "tool", "", "tool name")
	cmd.Flags().StringVar(&path, "path", "", "file path")
	cmd.Flags().StringVar(&command, "command", "", "command")
	cmd.Flags().StringVar(&url, "url", "", "URL")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "working directory")
	cmd.Flags().StringVar(&project, "project", "", "project name")
	cmd.Flags().IntVar(&totalActions, "context-total-actions", 0, "context total action count")
	cmd.Flags().IntVar(&filesRead, "context-files-read", 0, "context files read count")
	cmd.Flags().IntVar(&filesWritten, "context-files-written", 0, "context files written count")
	cmd.Flags().IntVar(&commandsExec, "context-commands-executed", 0, "context commands executed count")
	cmd.Flags().IntVar(&errorsCount, "context-errors", 0, "context error count")

	return cmd
}

type policyTestView struct {
	Sources        []string          `json:"sources"`
	Action         map[string]string `json:"action,omitempty"`
	Decision       string            `json:"decision"`
	MatchedRuleIDs []string          `json:"matched_rule_ids"`
	Message        string            `json:"message,omitempty"`
	Severity       string            `json:"severity,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
}

func actionSummary(a *aarmsec.Action) map[string]string {
	out := map[string]string{"type": string(a.Type)}
	if a.Tool != "" {
		out["tool"] = a.Tool
	}
	if a.Agent != "" {
		out["agent"] = a.Agent
	}
	if a.Project != "" {
		out["project"] = a.Project
	}
	if a.WorkingDir != "" {
		out["working_dir"] = a.WorkingDir
	}
	if a.Parameters.Path != "" {
		out["path"] = a.Parameters.Path
	}
	if a.Parameters.Command != "" {
		out["command"] = a.Parameters.Command
	}
	if a.Parameters.URL != "" {
		out["url"] = a.Parameters.URL
	}
	return out
}

func sourceNames(sources []loader.Source) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Name())
	}
	return out
}

func renderPolicyTest(w io.Writer, c *tui.Colorizer, v policyTestView, sources []loader.Source) {
	_, _ = fmt.Fprintf(w, "%s\n", c.Header("Policy evaluation"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(80))

	_, _ = fmt.Fprintf(w, "  %-12s %s %s\n",
		c.Dim("decision"),
		decorateDecision(c, v.Decision),
		decorateSeverity(c, v.Severity))

	if len(v.MatchedRuleIDs) == 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("matched"), c.Dim("(no rules matched)"))
	} else {
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("matched"), c.Cyan(strings.Join(v.MatchedRuleIDs, ", ")))
	}
	if len(v.Tags) > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("tags"), c.Dim(strings.Join(v.Tags, ", ")))
	}

	if len(v.Action) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, c.Header("Action"))
		for _, key := range orderedActionKeys(v.Action) {
			_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim(key), v.Action[key])
		}
	}

	if v.Message != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, c.Header("Message"))
		for _, line := range strings.Split(strings.TrimRight(v.Message, "\n"), "\n") {
			_, _ = fmt.Fprintf(w, "  %s\n", line)
		}
	}

	_, _ = fmt.Fprintln(w)
	renderSources(w, c, sources, false)
}

func decorateDecision(c *tui.Colorizer, d string) string {
	upper := strings.ToUpper(d)
	switch model.Decision(d) {
	case model.DecisionBlock:
		return c.Error(upper)
	case model.DecisionGuidance, model.DecisionWarn:
		return c.Warning(upper)
	case model.DecisionAllow:
		return c.Success(upper)
	default:
		return d
	}
}

func decorateSeverity(c *tui.Colorizer, sev string) string {
	if sev == "" {
		return ""
	}
	tag := fmt.Sprintf("(severity: %s)", sev)
	switch model.Severity(sev) {
	case model.SeverityCritical, model.SeverityHigh:
		return c.Error(tag)
	case model.SeverityMedium:
		return c.Warning(tag)
	default:
		return c.Dim(tag)
	}
}

func orderedActionKeys(m map[string]string) []string {
	order := []string{"type", "tool", "agent", "project", "working_dir", "path", "command", "url"}
	out := make([]string, 0, len(m))
	for _, k := range order {
		if _, ok := m[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func loadPolicyMediator(cfg *config.Config, paths *config.Paths) (*aarmsec.Mediator, error) {
	ldr, err := buildPolicyLoader(cfg, paths, "")
	if err != nil {
		return nil, err
	}
	policy, err := ldr.Load(context.Background())
	if err != nil {
		return nil, err
	}
	return aarmsec.NewMediator(policy)
}

// lazyPolicyCheck defers policy load until the first hook event so a broken
// policy file does not lock the user out of `gryph policy validate/test/sources`,
// the very commands they need to diagnose and fix it. Load errors propagate
// to the security evaluator, which applies policy.fail_mode. The first load
// failure is also recorded in the self-audit log so it surfaces under
// `gryph self-log` even when fail_mode=open silently allows the action.
type lazyPolicyCheck struct {
	cfg      *config.Config
	paths    *config.Paths
	getStore func() storage.Store

	once sync.Once
	med  *aarmsec.Mediator
	err  error
}

func newLazyPolicyCheck(cfg *config.Config, paths *config.Paths, getStore func() storage.Store) *lazyPolicyCheck {
	return &lazyPolicyCheck{cfg: cfg, paths: paths, getStore: getStore}
}

func (l *lazyPolicyCheck) load() (*aarmsec.Mediator, error) {
	l.once.Do(func() {
		l.med, l.err = loadPolicyMediator(l.cfg, l.paths)
		if l.err != nil {
			l.recordLoadFailure(l.err)
		}
	})
	return l.med, l.err
}

func (l *lazyPolicyCheck) recordLoadFailure(loadErr error) {
	if l.getStore == nil {
		return
	}
	store := l.getStore()
	if store == nil {
		return
	}
	details := map[string]interface{}{
		"fail_mode": l.cfg.EffectivePolicy().FailMode,
	}
	if err := logSelfAudit(context.Background(), store, SelfAuditActionPolicyLoadError, "",
		details, SelfAuditResultError, loadErr.Error()); err != nil {
		log.Errorf("failed to record policy load failure: %v", err)
	}
}

func (l *lazyPolicyCheck) Name() string { return aarmsec.CheckName }

func (l *lazyPolicyCheck) Enabled() bool {
	if l == nil || l.cfg == nil {
		return false
	}
	return l.cfg.EffectivePolicy().Enabled
}

func (l *lazyPolicyCheck) Check(ctx context.Context, event *events.Event) (*coresecurity.CheckResult, error) {
	med, err := l.load()
	if err != nil {
		return nil, fmt.Errorf("policy load failed: %w", err)
	}
	return med.Check(ctx, event)
}

var _ coresecurity.Check = (*lazyPolicyCheck)(nil)

func buildPolicyLoader(cfg *config.Config, paths *config.Paths, override string) (*loader.Loader, error) {
	if override != "" {
		return loader.New(loader.NewFileSource(override)), nil
	}

	var (
		policyCfg config.PolicyConfig
		hasCfg    bool
	)
	if cfg != nil {
		policyCfg = cfg.EffectivePolicy()
		hasCfg = true
	}

	var sources []loader.Source

	if policyCfg.ConventionalPaths {
		if cwd, err := os.Getwd(); err == nil {
			sources = append(sources, loader.NewConventionalSource(cwd))
		}
	}

	for _, p := range policyCfg.PolicyPaths {
		src, err := sourceForPath(p)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}

	if !hasCfg || len(policyCfg.PolicyPaths) == 0 {
		if paths == nil {
			paths = config.ResolvePaths()
		}
		fallback := filepath.Join(paths.ConfigDir, "policy.yaml")
		sources = append(sources, loader.NewOptionalFileSource(fallback))
	}

	return loader.New(sources...), nil
}

func sourceForPath(p string) (loader.Source, error) {
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return loader.NewFileSource(p), nil
		}
		return nil, fmt.Errorf("stat policy source %q: %w", p, err)
	}
	if info.IsDir() {
		return loader.NewDirSource(p), nil
	}
	return loader.NewFileSource(p), nil
}
