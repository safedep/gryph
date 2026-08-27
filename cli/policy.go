package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/classify"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/injectscore"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/schema"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

	receiptsCmd := newPolicyReceiptsCmd()
	receiptsCmd.AddCommand(newPolicyReceiptsExportCmd())
	receiptsCmd.AddCommand(newPolicyReceiptsVerifyLogCmd())

	cmd.AddCommand(
		newPolicyInitCmd(),
		newPolicyEditCmd(),
		newPolicyListCmd(),
		newPolicyInstallCmd(),
		newPolicySchemaCmd(),
		newPolicyBuiltinCmd(),
		newPolicyValidateCmd(),
		newPolicyTestCmd(),
		newPolicyContextCmd(),
		receiptsCmd,
		newPolicyApproveCmd(),
		newPolicyKeysCmd(),
		newPolicyDeferralsCmd(),
	)

	return cmd
}

func policyColorizer(app *App) *tui.Colorizer {
	if app == nil || app.Config == nil {
		return tui.NewColorizer(false)
	}
	return tui.NewColorizer(app.Config.ShouldUseColors())
}

func appPaths(app *App) *config.Paths {
	if app != nil && app.Paths != nil {
		return app.Paths
	}
	return config.ResolvePaths()
}

func appConfig(app *App) *config.Config {
	if app != nil {
		return app.Config
	}
	return nil
}

func writeExamplePolicy(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return ErrConfig("refusing to overwrite existing file (use --force to replace)", fmt.Errorf("%s exists", path))
		} else if !os.IsNotExist(err) {
			return ErrConfig("stat target path", err)
		}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ErrConfig("create parent directory", err)
		}
	}
	if err := os.WriteFile(path, []byte(pdp.ExampleYAML()), 0o644); err != nil {
		return ErrConfig("write policy file", err)
	}
	return nil
}

// policyTargetKind classifies the resolved target of init and edit.
type policyTargetKind int

const (
	policyTargetGlobal policyTargetKind = iota // the global policy.yaml
	policyTargetNamed                          // a named file in the policies directory
	policyTargetPath                           // a literal off-tree candidate path
)

type policyTarget struct {
	path string
	kind policyTargetKind
}

// managed reports whether the target lives in the config directory, so an
// author change to it is a live policy change worth a self-audit row.
func (t policyTarget) managed() bool {
	return t.kind == policyTargetGlobal || t.kind == policyTargetNamed
}

// resolvePolicyTarget maps the optional init/edit argument to a filesystem
// target. A bare name resolves into the policies directory with a normalized
// .yaml extension. A path (a separator, or a leading ., ~, or /) resolves
// literally with ~ expanded. No argument targets the global policy file.
func resolvePolicyTarget(paths *config.Paths, args []string) (policyTarget, error) {
	if len(args) == 0 || args[0] == "" {
		return policyTarget{path: config.DefaultPolicyFilePath(paths), kind: policyTargetGlobal}, nil
	}
	arg := args[0]
	if isPolicyPathArg(arg) {
		p, err := expandUserPath(arg)
		if err != nil {
			return policyTarget{}, err
		}
		return policyTarget{path: p, kind: policyTargetPath}, nil
	}
	name := loader.NormalizePolicyFileName(arg)
	return policyTarget{path: filepath.Join(config.DefaultPolicyDirPath(paths), name), kind: policyTargetNamed}, nil
}

// isPolicyPathArg reports whether the argument is a path rather than a bare
// name. A path holds a separator or starts with ., ~, or /.
func isPolicyPathArg(arg string) bool {
	if strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~") || strings.HasPrefix(arg, "/") {
		return true
	}
	return strings.ContainsRune(arg, '/') || strings.ContainsRune(arg, filepath.Separator)
}

// expandUserPath expands a leading ~ to the user home directory.
func expandUserPath(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ErrConfig("resolve home directory", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

// auditPolicyChange records a policy authoring action in the self-audit log so
// every live policy change leaves a trail. It reuses the config_change action
// and best-effort store, so a missing database never blocks authoring.
func auditPolicyChange(ctx context.Context, app *App, operation, path string) {
	if app == nil {
		return
	}
	closeStore := openAuditStore(ctx, app)
	defer closeStore()
	if app.Store == nil {
		return
	}
	details := map[string]interface{}{"operation": operation, "path": path}
	if err := logSelfAudit(ctx, app.Store, SelfAuditActionConfigChange, "", details, SelfAuditResultSuccess, ""); err != nil {
		log.Warnf("failed to record %s self-audit: %v", operation, err)
	}
}

// openAuditStore opens the store on the App best-effort and returns a closer.
// It mirrors the config-command pattern so authoring commands can audit
// without a hard database dependency.
func openAuditStore(ctx context.Context, app *App) func() {
	if app == nil || app.Store != nil {
		return func() {}
	}
	if err := config.EnsureDirectories(); err != nil {
		return func() {}
	}
	if err := app.InitStore(ctx); err != nil {
		return func() {}
	}
	return func() {
		if err := app.Close(); err != nil {
			log.Errorf("failed to close app: %v", err)
		}
	}
}

func resolveEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

func runEditor(editor, path string) error {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	args := append(fields[1:], path)
	c := exec.Command(fields[0], args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func newPolicyInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [name|path]",
		Short: "Write the example policy to a policy file",
		Long: "Writes the embedded, fully commented example policy to a target. " +
			"No argument targets the global policy.yaml. A bare name targets " +
			"<name>.yaml in the policies directory. A path (with a separator or a " +
			"leading ., ~, or /) targets that literal file, which is a candidate " +
			"for a human to review and `gryph policy install`.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy init, using resolved defaults: %v", err)
			}
			target, err := resolvePolicyTarget(appPaths(app), args)
			if err != nil {
				return err
			}
			if err := writeExamplePolicy(target.path, force); err != nil {
				return err
			}
			if target.managed() {
				auditPolicyChange(cmd.Context(), app, "policy_init", target.path)
			}

			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "%s Wrote example policy to %s\n", c.StatusOK(), c.Path(target.path)); err != nil {
				return err
			}
			if target.managed() {
				warnMergedPolicyConflict(cmd, app)
			}
			return printInitNextStep(out, c, target)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the target file if it already exists")
	return cmd
}

// warnMergedPolicyConflict warns when the merged policy no longer loads after a
// scaffold write into the config directory, for example because the example
// reuses rule IDs that another file already defines. It does not fail. The file
// is written, and the author edits the IDs to make them unique.
func warnMergedPolicyConflict(cmd *cobra.Command, app *App) {
	if _, err := buildPolicyLoader(appConfig(app), appPaths(app)).Load(cmd.Context()); err != nil {
		c := policyColorizer(app)
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "  %s %s\n", c.Warning("Warning:"), firstLine(err.Error()))
		_, _ = fmt.Fprintf(out, "  %s\n", c.Dim("Edit the file so every rule ID is unique across your policy files."))
	}
}

func printInitNextStep(out io.Writer, c *tui.Colorizer, target policyTarget) error {
	hint := "Next: `gryph policy edit` to customize, then `gryph policy validate`"
	if target.kind == policyTargetPath {
		hint = "This is a candidate. Ask a human to review and `gryph policy install " + target.path + "`."
	}
	_, err := fmt.Fprintf(out, "  %s\n", c.Dim(hint))
	return err
}

func newPolicyEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit [name|path]",
		Short: "Open a Gryph policy file in your editor",
		Long: "Opens a policy file in $VISUAL or $EDITOR, falling back to vi. No " +
			"argument targets the global policy.yaml. A bare name targets " +
			"<name>.yaml in the policies directory. A path targets that literal " +
			"file. A missing file is scaffolded from the example first. After the " +
			"editor exits Gryph validates the result and prints it. A name or the " +
			"global file validates the merged policy. A path validates that file " +
			"alone. A validation failure exits non-zero and leaves the file as " +
			"written.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy edit, using resolved defaults: %v", err)
			}
			target, err := resolvePolicyTarget(appPaths(app), args)
			if err != nil {
				return err
			}
			if _, statErr := os.Stat(target.path); os.IsNotExist(statErr) {
				if werr := writeExamplePolicy(target.path, false); werr != nil {
					return werr
				}
			}
			if err := runEditor(resolveEditor(), target.path); err != nil {
				return ErrConfig("run editor", err)
			}
			if target.managed() {
				auditPolicyChange(cmd.Context(), app, "policy_edit", target.path)
			}
			if target.kind == policyTargetPath {
				return reportFileValidation(cmd, app, target.path)
			}
			return reportPolicyValidation(cmd, app)
		},
	}
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

func newPolicyBuiltinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "builtin",
		Short: "Print the built-in self-protection rules",
		Long: "Prints the AARM self-protection rules compiled into Gryph as YAML. " +
			"These rules block agent writes to Gryph's own policy files, config, " +
			"database, signing keys, and the agents' hook configs. They load last, " +
			"cannot be disabled by a repo-local policy, and are toggled only via " +
			"policy.self_protection.enabled in the operator's config file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy builtin, using resolved defaults: %v", err)
			}
			var cfg *config.Config
			var paths *config.Paths
			if app != nil {
				cfg = app.Config
				paths = app.Paths
			}
			if paths == nil {
				paths = config.ResolvePaths()
			}

			src := loader.NewBuiltinSource(selfProtectionGlobs(cfg, paths)...)
			docs, err := src.Load(cmd.Context())
			if err != nil {
				return ErrConfig("build self-protection rules", err)
			}
			out := cmd.OutOrStdout()
			for _, doc := range docs {
				data, err := yaml.Marshal(doc)
				if err != nil {
					return ErrConfig("marshal self-protection rules", err)
				}
				if _, err := out.Write(data); err != nil {
					return err
				}
			}
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
	sourceStatusFound       = "found"
	sourceStatusMissing     = "missing"
	sourceStatusUnreadable  = "unreadable"
	sourceStatusUnknown     = "unknown"
	sourceStatusIsDirectory = "is a directory"
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
		return sourceRow{Kind: "dir", Path: s.Path, Optional: s.Optional, Status: status, Problem: problem}
	case *loader.BuiltinSource:
		return sourceRow{
			Kind:     "builtin",
			Path:     "self-protection",
			Optional: false,
			Status:   sourceStatusFound,
			Hints: []string{
				"Built-in rules protecting Gryph policy files, config, database, keys, and agent hook configs",
				"Always loaded last; not affected by user disabled: lists",
				"Toggle with policy.self_protection.enabled in the config file",
			},
		}
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
	info, err := os.Stat(path)
	if err != nil {
		return missingStatus(err, optional)
	}
	if !info.IsDir() {
		return "not a directory", true
	}
	return sourceStatusFound, false
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
	case status == sourceStatusFound, strings.HasPrefix(status, sourceStatusFound+": "):
		return c.Success(status)
	case status == sourceStatusMissing || status == sourceStatusUnknown:
		return c.Dim(status)
	default:
		return status
	}
}

func reportPolicyValidation(cmd *cobra.Command, app *App) error {
	ldr := buildPolicyLoader(appConfig(app), appPaths(app))
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
}

// reportFileValidation validates one policy file in isolation, without
// resolving or merging the active policy. It is the shared check behind
// `validate --file`, `edit <path>`, and the candidate step of `install`.
func reportFileValidation(cmd *cobra.Command, app *App, path string) error {
	policy, err := pdp.LoadPolicyFile(path)
	if err != nil {
		return ErrConfig("failed to validate policy file", err)
	}
	c := policyColorizer(app)
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n",
		c.StatusOK(),
		c.Success(fmt.Sprintf("File valid: %d rules in %s", len(policy.Rules), path)))
	return err
}

func newPolicyValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the merged Gryph policy, or one file with --file",
		Long: "Validates the merged active policy (global file, policies directory, " +
			"and built-ins). With --file, validates one policy file in isolation, " +
			"without resolving or merging the active policy. Use --file to lint a " +
			"candidate before install.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			if file != "" {
				path, err := expandUserPath(file)
				if err != nil {
					return err
				}
				return reportFileValidation(cmd, app, path)
			}
			return reportPolicyValidation(cmd, app)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "validate a single policy file in isolation")
	return cmd
}

func newPolicyTestCmd() *cobra.Command {
	var (
		format       string
		file         string
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
		Short: "Dry-run a synthetic action through the merged policy, or one file with --file",
		Long: "Dry-runs a synthetic action through the active merged policy. With " +
			"--file, dry-runs against one policy file plus the built-in rules, " +
			"instead of the active policy. Use --file to check a draft in a " +
			"workspace directory before you install it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			ldr, err := testPolicyLoader(app, file)
			if err != nil {
				return err
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

			got := identity.NewDefaultCapturer().Capture(context.Background())
			action := &aarmsec.Action{
				Type:            at,
				Tool:            tool,
				Agent:           agentName,
				WorkingDir:      workingDir,
				Project:         project,
				HumanPrincipal:  got.HumanPrincipal,
				ServiceIdentity: got.ServiceIdentity,
				RoleScope:       got.RoleScope,
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

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().StringVar(&file, "file", "", "dry-run against one policy file plus the built-in rules, instead of the active policy")
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
	if a.HumanPrincipal != "" {
		out["human_principal"] = a.HumanPrincipal
	}
	if a.ServiceIdentity != "" {
		out["service_identity"] = a.ServiceIdentity
	}
	if a.RoleScope != "" {
		out["role_scope"] = a.RoleScope
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
	case model.DecisionEscalate, model.DecisionDefer:
		return c.Warning(upper)
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
	order := []string{"type", "tool", "agent", "project", "working_dir", "path", "command", "url", "human_principal", "service_identity", "role_scope"}
	out := make([]string, 0, len(m))
	for _, k := range order {
		if _, ok := m[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func loadPolicyMediator(cfg *config.Config, paths *config.Paths, store storage.Store) (*aarmsec.Mediator, error) {
	ldr := buildPolicyLoader(cfg, paths)
	policy, err := ldr.Load(context.Background())
	if err != nil {
		return nil, err
	}
	var opts []aarmsec.MediatorOption
	if store != nil {
		opts = append(opts, aarmsec.WithAccumulator(accumulator.NewSQLite(store)))
		var recOpts []receipt.GeneratorOption
		if cfg != nil && cfg.Policy.Receipts.EffectiveSignMode() != config.SignModeNever {
			signer, signErr := loadReceiptSignerFromConfig(cfg, paths)
			if signErr != nil {
				return nil, fmt.Errorf("load receipt signer: %w", signErr)
			}
			if signer != nil {
				recOpts = append(recOpts, receipt.WithSigner(signer))
			}
		}
		base := receipt.NewSQLite(store, recOpts...)
		opts = append(opts, aarmsec.WithReceiptGenerator(newAuditingReceiptGenerator(base, store)))
	}
	if cfg != nil {
		policyCfg := cfg.EffectivePolicy()
		opts = append(opts, aarmsec.WithMediatorConfig(aarmsec.MediatorConfig{
			LogAllEvaluations: policyCfg.LogAllEvaluations,
			ApprovalTimeout:   time.Duration(policyCfg.Approval.TimeoutSeconds) * time.Second,
		}))

		var classifier classify.Classifier
		if policyCfg.Classify.Enabled {
			secretPaths := cfg.Privacy.SensitivePaths
			if len(secretPaths) == 0 {
				secretPaths = events.DefaultSensitivePatterns()
			}
			classifyOpts := []classify.HeuristicOption{classify.WithSecretPaths(secretPaths)}
			if len(policyCfg.Classify.ExtraPatterns) > 0 {
				classifyOpts = append(classifyOpts, classify.WithExtraPatterns(policyCfg.Classify.ExtraPatterns))
			}
			classifier = classify.NewHeuristic(classifyOpts...)
		}
		if !policyCfg.Classify.FailOpen {
			classifier = classify.NewFailSafe(classifier, classify.LabelUnknownSensitive)
		}

		var adapterOpts []mediation.CommonOption
		if classifier != nil {
			adapterOpts = append(adapterOpts, mediation.WithClassifier(classifier))
		}

		if policyCfg.InjectionScore.Enabled {
			adapterOpts = append(adapterOpts, mediation.WithInjectionScorer(injectscore.NewHeuristic()))
		}

		var identityCapturer identity.Capturer
		if policyCfg.Identity.Enabled {
			identityCapturer = identity.NewDefaultCapturer()
		} else {
			identityCapturer = identity.NewStaticCapturer(identity.Capture{})
		}
		adapterOpts = append(adapterOpts, mediation.WithIdentityCapturer(identityCapturer))

		opts = append(opts, aarmsec.WithAdapter(mediation.NewHookAdapter(adapterOpts...)))
		opts = append(opts, aarmsec.WithIdentityConfig(aarmsec.IdentityConfig{
			Enabled:               policyCfg.Identity.Enabled,
			RequireHumanPrincipal: policyCfg.Identity.RequireHumanPrincipal,
		}))
		if store != nil {
			opts = append(opts, aarmsec.WithIdentityAuditHook(newIdentityAuditHook(store)))
		}

		switch policyCfg.Approval.Mode {
		case config.ApprovalModeCLI:
			opts = append(opts, aarmsec.WithApprovalService(approval.NewCLIPrompt(
				approval.WithRequireNote(policyCfg.Approval.RequireNote),
			)))
		default:
			opts = append(opts, aarmsec.WithApprovalService(approval.NewNop()))
		}

		if store != nil {
			opts = append(opts, aarmsec.WithApprovalAuditHook(newApprovalAuditHook(store)))
		}

		opts = append(opts, aarmsec.WithDeferralConfig(aarmsec.DeferralConfig{
			Enabled:               policyCfg.Defer.Enabled,
			TimeoutSeconds:        policyCfg.Defer.TimeoutSeconds,
			FreshSessionSeconds:   policyCfg.Defer.FreshSessionSeconds,
			ConflictTriggersDefer: policyCfg.Defer.ConflictTriggersDefer,
		}))
		if store != nil {
			opts = append(opts, aarmsec.WithDeferralHook(newDeferralHook(store)))
		}
	}
	return aarmsec.NewMediator(policy, opts...)
}

// auditingReceiptGenerator wraps a receipt.Generator and emits a
// receipt_signed self-audit row after each signed receipt insert completes.
// The audit is emitted after the receipt's write transaction has committed
// so the audit insert does not contend with the receipt insert for the
// SQLite writer lock.
type auditingReceiptGenerator struct {
	inner receipt.Generator
	store storage.Store
}

func newAuditingReceiptGenerator(inner receipt.Generator, store storage.Store) *auditingReceiptGenerator {
	return &auditingReceiptGenerator{inner: inner, store: store}
}

func (a *auditingReceiptGenerator) Record(ctx context.Context, in *receipt.RecordInput) (*receipt.Record, error) {
	rec, err := a.inner.Record(ctx, in)
	if err != nil {
		return rec, err
	}
	if rec != nil && rec.SignerKeyID != "" && a.store != nil {
		details := map[string]interface{}{
			"key_id":     rec.SignerKeyID,
			"session_id": in.SessionID.String(),
			"sequence":   rec.Sequence,
		}
		if logErr := logSelfAudit(ctx, a.store, SelfAuditActionReceiptSigned, "",
			details, SelfAuditResultSuccess, ""); logErr != nil {
			log.Errorf("failed to record receipt_signed audit: %v", logErr)
		}
	}
	return rec, nil
}

func (a *auditingReceiptGenerator) UpdateResult(ctx context.Context, sessionID uuid.UUID, sequence int64, result model.Result) error {
	return a.inner.UpdateResult(ctx, sessionID, sequence, result)
}

func (a *auditingReceiptGenerator) UpdateDecision(ctx context.Context, sessionID uuid.UUID, sequence int64, decision string, resultStatus string, note string) error {
	return a.inner.UpdateDecision(ctx, sessionID, sequence, decision, resultStatus, note)
}

// newDeferralHook returns the Mediator DeferralHook that persists the
// pending-deferral row, emits the deferral_requested self-audit row, and
// renders the CLI-shaped operator hint the Mediator splices into the
// agent-facing block message. Keeping the hint here (instead of inside
// aarm) lets aarm stay decoupled from CLI command spellings.
func newDeferralHook(store storage.Store) aarmsec.DeferralHook {
	return func(ctx context.Context, r aarmsec.DeferralRecord) (uuid.UUID, string, error) {
		if store == nil {
			return uuid.Nil, "", nil
		}
		row := &storage.DeferredActionRow{
			ID:              uuid.New(),
			SessionID:       r.SessionID,
			ReceiptSequence: r.ReceiptSequence,
			ActionID:        r.ActionID,
			DeferredAt:      r.DeferredAt,
			ExpiresAt:       r.ExpiresAt,
			Reason:          r.Reason,
			Status:          storage.DeferredActionStatusPending,
		}
		if err := store.InsertDeferredAction(ctx, row); err != nil {
			log.Errorf("failed to insert deferred action: %v", err)
			return uuid.Nil, "", err
		}
		details := map[string]interface{}{
			"session_id":         r.SessionID.String(),
			"action_id":          r.ActionID.String(),
			"receipt_sequence":   r.ReceiptSequence,
			"deferred_action_id": row.ID.String(),
			"reason":             r.Reason,
			"expires_at":         r.ExpiresAt.Format(time.RFC3339),
		}
		agent := ""
		if r.Action != nil {
			agent = r.Action.Agent
			details["agent"] = r.Action.Agent
			details["tool"] = r.Action.Tool
			details["action_type"] = string(r.Action.Type)
		}
		if r.Decision != nil {
			details["matched_rule_ids"] = r.Decision.MatchedRuleIDs
		}
		if err := logSelfAudit(ctx, store, SelfAuditActionDeferralRequested, agent,
			details, SelfAuditResultSuccess, ""); err != nil {
			log.Errorf("failed to record deferral_requested audit: %v", err)
		}
		hint := fmt.Sprintf("Resolve with `gryph policy deferrals resolve --id %s`.",
			shortDeferralID(row.ID))
		return row.ID, hint, nil
	}
}

// shortDeferralID renders the 8-character prefix used in operator hints.
// Falls back to the full string for IDs that are somehow shorter so the
// hint always references something the operator can act on.
func shortDeferralID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

func newApprovalAuditHook(store storage.Store) aarmsec.ApprovalAuditHook {
	return func(ctx context.Context, e aarmsec.ApprovalAudit) {
		if store == nil {
			return
		}
		details := map[string]interface{}{}
		agentName := ""
		if e.Request != nil {
			details["session_id"] = e.Request.SessionID.String()
			details["action_id"] = e.Request.ActionID.String()
			if e.Request.Action != nil {
				agentName = e.Request.Action.Agent
				details["agent"] = e.Request.Action.Agent
				details["tool"] = e.Request.Action.Tool
				details["action_type"] = string(e.Request.Action.Type)
			}
		}
		if e.Decision != nil {
			details["matched_rule_ids"] = e.Decision.MatchedRuleIDs
		}
		if e.Outcome != nil {
			details["approver"] = e.Outcome.Approver
			if e.Outcome.Note != "" {
				details["note"] = e.Outcome.Note
			}
		}
		result := SelfAuditResultSuccess
		errMsg := ""
		if e.Error != nil {
			result = SelfAuditResultError
			errMsg = e.Error.Error()
		}
		if e.Action == SelfAuditActionApprovalDenied || e.Action == SelfAuditActionApprovalTimeout {
			result = SelfAuditResultSkipped
		}
		if err := logSelfAudit(ctx, store, e.Action, agentName, details, result, errMsg); err != nil {
			log.Errorf("failed to record %s audit: %v", e.Action, err)
		}
	}
}

// newIdentityAuditHook returns a Mediator IdentityAuditHook that emits the
// identity_missing self-audit row on the pre-PDP block path.
func newIdentityAuditHook(store storage.Store) aarmsec.IdentityAuditHook {
	return func(ctx context.Context, e aarmsec.IdentityAudit) {
		if store == nil {
			return
		}
		details := map[string]interface{}{}
		agentName := ""
		if e.Action != nil {
			agentName = e.Action.Agent
			details["session_id"] = e.Action.SessionID.String()
			details["action_id"] = e.Action.ID.String()
			details["agent"] = e.Action.Agent
			details["tool"] = e.Action.Tool
			details["action_type"] = string(e.Action.Type)
		}
		errMsg := ""
		if e.Decision != nil {
			errMsg = e.Decision.Reason
		}
		if err := logSelfAudit(ctx, store, SelfAuditActionIdentityMissing, agentName,
			details, SelfAuditResultError, errMsg); err != nil {
			log.Errorf("failed to record identity_missing audit: %v", err)
		}
	}
}

// lazyPolicyCheck defers policy load until the first hook event so a broken
// policy file does not lock the user out of `gryph policy validate/test`,
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
		var store storage.Store
		if l.getStore != nil {
			store = l.getStore()
		}
		l.med, l.err = loadPolicyMediator(l.cfg, l.paths, store)
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

// Mediator returns the underlying aarm Mediator if it has been loaded
// successfully. Returns nil if the policy has not yet been loaded or load
// failed. Used by cli/hook.go to drive post-hook RecordResult calls.
func (l *lazyPolicyCheck) Mediator() *aarmsec.Mediator {
	if l == nil {
		return nil
	}
	return l.med
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
	result, checkErr := med.Check(ctx, event)
	if checkErr != nil {
		if errors.Is(checkErr, accumulator.ErrSnapshot) {
			l.recordAarmFailure(event, SelfAuditActionContextSnapshotError, "accumulator snapshot", checkErr)
		}
		if errors.Is(checkErr, receipt.ErrInsert) {
			l.recordAarmFailure(event, SelfAuditActionReceiptInsertError, "receipt insert", checkErr)
		}
	}
	return result, checkErr
}

// recordAarmFailure is the shared shape behind every AARM-component self-audit
// emission. label is the short human string used in the fallback log line
// when no store is available. action selects the SelfAudit action constant.
func (l *lazyPolicyCheck) recordAarmFailure(event *events.Event, action, label string, recErr error) {
	if l.getStore == nil {
		log.Warnf("aarm: %s failure (no store): %v", label, recErr)
		return
	}
	store := l.getStore()
	if store == nil {
		log.Warnf("aarm: %s failure (nil store): %v", label, recErr)
		return
	}
	details := map[string]interface{}{}
	agentName := ""
	if event != nil {
		details["session_id"] = event.SessionID.String()
		details["agent"] = event.AgentName
		agentName = event.AgentName
	}
	details["error"] = recErr.Error()
	if err := logSelfAudit(context.Background(), store, action, agentName,
		details, SelfAuditResultError, recErr.Error()); err != nil {
		log.Errorf("failed to record %s failure: %v", label, err)
	}
}

var _ coresecurity.Check = (*lazyPolicyCheck)(nil)

func buildPolicyLoader(cfg *config.Config, paths *config.Paths) *loader.Loader {
	return loader.New(policyLoaderSources(cfg, paths)...)
}

// policyLoaderSources returns the ordered policy sources: the global file, the
// policies directory, and (when self-protection is on) the built-in source.
// It is the single definition of the resolution order shared by the loader and
// the inspection commands.
func policyLoaderSources(cfg *config.Config, paths *config.Paths) []loader.Source {
	if paths == nil {
		paths = config.ResolvePaths()
	}
	sources := []loader.Source{
		loader.NewOptionalFileSource(config.DefaultPolicyFilePath(paths)),
		loader.NewOptionalDirSource(config.DefaultPolicyDirPath(paths)),
	}
	return appendBuiltinSource(sources, cfg, paths)
}

// appendBuiltinSource adds the self-protection source when it is enabled.
func appendBuiltinSource(sources []loader.Source, cfg *config.Config, paths *config.Paths) []loader.Source {
	if selfProtectionEnabled(cfg) {
		sources = append(sources, loader.NewBuiltinSource(selfProtectionGlobs(cfg, paths)...))
	}
	return sources
}

func selfProtectionEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.EffectivePolicy().SelfProtection.Enabled
}

// testPolicyLoader builds the loader for `policy test`. With no file, it returns
// the active merged policy. With a file, it returns that one file plus the
// built-in rules, so an author can dry-run a workspace draft with faithful
// precedence, without resolving the active policy.
func testPolicyLoader(app *App, file string) (*loader.Loader, error) {
	if file == "" {
		return buildPolicyLoader(appConfig(app), appPaths(app)), nil
	}
	p, err := expandUserPath(file)
	if err != nil {
		return nil, err
	}
	sources := appendBuiltinSource([]loader.Source{loader.NewFileSource(p)}, appConfig(app), appPaths(app))
	return loader.New(sources...), nil
}

func selfProtectionGlobs(cfg *config.Config, paths *config.Paths) []string {
	var globs []string
	if paths != nil && paths.ConfigDir != "" {
		globs = append(globs, filepath.ToSlash(paths.ConfigDir)+"/**")
	}
	if cfg != nil {
		if db := cfg.GetDatabasePath(); db != "" {
			globs = append(globs, filepath.ToSlash(db))
		}
		globs = append(globs,
			filepath.ToSlash(cfg.ResolveReceiptKeyPath(paths)),
			filepath.ToSlash(cfg.ResolveReceiptTrustStorePath(paths)),
		)
	}
	globs = append(globs, agent.HookConfigGlobs()...)
	return globs
}
