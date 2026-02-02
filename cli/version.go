package cli

import (
	"fmt"

	"github.com/safedep/gryph/internal/version"
	"github.com/spf13/cobra"
)

func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "gryph %s\n", version.Version)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "commit: %s\n", version.Commit)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "https://github.com/safedep/gryph\n")

			return nil
		},
	}

	return cmd
}
