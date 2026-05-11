package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/spf13/cobra"
)

// NewPolicyCmd creates the policy command group.
func NewPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Validate and test security policies",
	}

	cmd.AddCommand(
		newPolicyValidateCmd(),
		newPolicyTestCmd(),
	)

	return cmd
}

func newPolicyValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a policy file",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			path := file
			if path == "" {
				path = defaultPolicyPath(app.Config, app.Paths)
			}

			policy, err := pdp.LoadPolicyFile(path)
			if err != nil {
				return ErrConfig("failed to validate policy", err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Policy valid: %s (%d rules)\n", path, len(policy.Rules))
			return err
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "policy file to validate")
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
		Short: "Dry-run a synthetic action through a policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			policyPath := file
			if policyPath == "" {
				policyPath = defaultPolicyPath(app.Config, app.Paths)
			}

			policy, err := pdp.LoadPolicyFile(policyPath)
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
				Policy:         policyPath,
				Decision:       string(result.Decision),
				MatchedRuleIDs: result.MatchedRuleIDs,
				Message:        result.Message,
				Severity:       result.Severity,
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

	cmd.Flags().StringVar(&file, "file", "", "policy file to test")
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
	Policy         string   `json:"policy"`
	Decision       string   `json:"decision"`
	MatchedRuleIDs []string `json:"matched_rule_ids"`
	Message        string   `json:"message,omitempty"`
	Severity       string   `json:"severity,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

func loadPolicyMediator(cfg *config.Config, paths *config.Paths) (*aarmsec.Mediator, error) {
	policyPath := defaultPolicyPath(cfg, paths)
	policy, err := pdp.LoadPolicyFile(policyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			policy = &pdp.Policy{Rules: []pdp.Rule{}}
		} else {
			return nil, err
		}
	}
	return aarmsec.NewMediator(policy)
}

func defaultPolicyPath(cfg *config.Config, paths *config.Paths) string {
	if cfg != nil {
		policyCfg := cfg.EffectivePolicy()
		if policyCfg.PolicyPath != "" {
			return policyCfg.PolicyPath
		}
	}
	if paths == nil {
		paths = config.ResolvePaths()
	}
	return filepath.Join(paths.ConfigDir, "policy.yaml")
}
