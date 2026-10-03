package cli

import (
	"context"
	"encoding/json"
	"os"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/spf13/cobra"
)

// userUninstallReport is what the per-user helper of gryph uninstall
// --managed prints: one row per agent on the host for this user.
type userUninstallReport struct {
	Agents []userAgentUninstall `json:"agents"`
	// Purged lists the per-user Gryph directories the helper removed.
	Purged []string `json:"purged,omitempty"`
}

type userAgentUninstall struct {
	Name         string   `json:"name"`
	HooksRemoved []string `json:"hooks_removed,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// newUninstallUserCmd is the hidden helper that gryph uninstall --managed
// runs once per account, as that account. It removes the Gryph entries
// from the agent hook files in the home under the repair rules, opens no
// database, and runs no agent program. With --purge it also removes the
// per-user Gryph directories.
func newUninstallUserCmd() *cobra.Command {
	var (
		purge  bool
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:    "_uninstall-user",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runUserUninstall(utils.WithoutProgramExecution(context.Background()), purge, dryRun)
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(report)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove the per-user Gryph directories")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report without changing anything")
	return cmd
}

func runUserUninstall(ctx context.Context, purge, dryRun bool) *userUninstallReport {
	report := &userUninstallReport{Agents: []userAgentUninstall{}}
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, config.Default())
	for _, adapter := range registry.All() {
		detection, err := adapter.Detect(ctx)
		if err != nil || detection == nil || !detection.Installed {
			continue
		}
		row := userAgentUninstall{Name: adapter.Name()}
		result, err := adapter.Uninstall(ctx, agent.UninstallOptions{DryRun: dryRun, Repair: true})
		switch {
		case err != nil:
			row.Error = err.Error()
		case result != nil && result.Error != nil:
			row.Error = result.Error.Error()
		case result != nil:
			row.HooksRemoved = result.HooksRemoved
		}
		report.Agents = append(report.Agents, row)
	}
	if purge && !dryRun {
		paths := config.ResolvePaths()
		for _, dir := range []string{paths.DataDir, paths.ConfigDir, paths.CacheDir} {
			if dir == "" {
				continue
			}
			if err := os.RemoveAll(dir); err == nil {
				report.Purged = append(report.Purged, dir)
			}
		}
	}
	return report
}
