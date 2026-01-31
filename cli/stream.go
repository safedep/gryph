package cli

import (
	"fmt"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/stream"
	"github.com/safedep/gryph/stream/stdout"
	"github.com/spf13/cobra"
)

// NewStreamCmd creates the hidden stream parent command.
func NewStreamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stream",
		Short:  "Stream event management",
		Hidden: true,
	}

	cmd.AddCommand(newStreamSyncCmd())
	return cmd
}

func newStreamSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync events to configured stream targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			defer app.Close()

			ctx := cmd.Context()
			if err := app.InitStore(ctx); err != nil {
				return err
			}

			registry := buildStreamRegistry(app.Config)
			if len(registry.Enabled()) == 0 {
				fmt.Println("No enabled stream targets configured.")
				return nil
			}

			syncer := stream.NewSyncer(app.Store, registry)
			result, err := syncer.Sync(ctx)
			if err != nil {
				return err
			}

			for _, tr := range result.TargetResults {
				if tr.Error != nil {
					fmt.Printf("[%s] error: %v\n", tr.TargetName, tr.Error)
				} else {
					fmt.Printf("[%s] synced %d events, %d audit entries\n",
						tr.TargetName, tr.EventsSent, tr.AuditsSent)
				}
			}

			return nil
		},
	}
}

func buildStreamRegistry(cfg *config.Config) *stream.Registry {
	registry := stream.NewRegistry()
	for _, tc := range cfg.Streams.Targets {
		switch tc.Type {
		case stream.TargetTypeStdout:
			registry.Register(stdout.New(tc.Name, tc.Enabled))
		}
	}

	return registry
}
