package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewMCPProxyCmd returns the `gryph mcp-proxy` stub command. Phase 4 only
// ships the MCP adapter contract (aarm/mediation/mcpadapter.go) and a CLI
// stub that prints a preview banner. The full JSON-RPC over stdio proxy
// lands in Phase 5.
func NewMCPProxyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-proxy",
		Short: "MCP proxy (preview, not yet functional)",
		Long: "MCP proxy stub. Phase 4 lands the adapter contract " +
			"(aarm/mediation/mcpadapter.go) but the JSON-RPC over stdio " +
			"proxy itself is deferred to Phase 5. See " +
			"docs/specs/2026-05-16-aarm-phase-4-spec.md for the design.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "gryph mcp-proxy: preview, not yet functional")
			_, _ = fmt.Fprintln(out, "  The MCP adapter contract is implemented in aarm/mediation/mcpadapter.go.")
			_, _ = fmt.Fprintln(out, "  The JSON-RPC over stdio proxy is deferred to Phase 5.")
			return nil
		},
	}
}
