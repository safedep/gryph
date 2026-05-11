package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/spf13/cobra"
)

func NewPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage Gryph security policies",
		Long: "Author, inspect, and operate Gryph's security policy layer. " +
			"Subcommands cover scaffolding (init), validation, dry-run testing, " +
			"and — as the AARM implementation grows — receipts, context, and " +
			"approval workflows.",
	}

	cmd.AddCommand(
		newPolicyInitCmd(),
		newPolicyValidateCmd(),
		newPolicyTestCmd(),
	)

	return cmd
}

func newPolicyInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init <path>",
		Short: "Write a fully-documented example policy to the given path",
		Long: "Writes the embedded, fully-commented example policy to <path>. " +
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

			fmt.Fprintf(cmd.OutOrStdout(), "Wrote example policy to %s\n", target)
			fmt.Fprintf(cmd.OutOrStdout(), "Next: edit the file, then `gryph policy validate --file %s`\n", target)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the target path if it already exists")
	return cmd
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

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Policy valid: %d rules from %d source(s)\n", len(policy.Rules), len(ldr.Sources()))
			for _, src := range ldr.Sources() {
				fmt.Fprintf(out, "  - %s\n", src.Name())
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

			sources := make([]string, 0, len(ldr.Sources()))
			for _, s := range ldr.Sources() {
				sources = append(sources, s.Name())
			}
			view := policyTestView{
				Sources:        sources,
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

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Decision: %s\nMatched rules: %v\nMessage: %s\n", view.Decision, view.MatchedRuleIDs, view.Message)
			return err
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
	Sources        []string `json:"sources"`
	Decision       string   `json:"decision"`
	MatchedRuleIDs []string `json:"matched_rule_ids"`
	Message        string   `json:"message,omitempty"`
	Severity       string   `json:"severity,omitempty"`
	Tags           []string `json:"tags,omitempty"`
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

// buildPolicyLoader assembles the policy loader from config. When override is
// non-empty it replaces all configured sources with a single required file
// source (used by `gryph policy {validate,test} --file`).
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
