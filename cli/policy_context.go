package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func newPolicyContextCmd() *cobra.Command {
	var (
		sessionID string
		limit     int
		format    string
	)

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Inspect AARM Context Accumulator state",
		Long: "Print the per-session counters captured by the Context Accumulator. " +
			"Without --session, lists every session that has a context state row, " +
			"ordered by last_action_at descending. With --session, prints the snapshot " +
			"for that session and the most recent action rows.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()

			c := policyColorizer(app)
			out := cmd.OutOrStdout()

			if sessionID != "" {
				return renderPolicyContextSession(ctx, out, c, app.Store, sessionID, limit, format)
			}
			return renderPolicyContextList(ctx, out, c, app.Store, limit, format)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to inspect")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of action rows or sessions to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	return cmd
}

type policyContextStateView struct {
	SessionID        string   `json:"session_id"`
	FirstSeenAt      string   `json:"first_seen_at"`
	LastActionAt     string   `json:"last_action_at"`
	TotalActions     int      `json:"total_actions"`
	FilesRead        int      `json:"files_read"`
	FilesWritten     int      `json:"files_written"`
	CommandsExecuted int      `json:"commands_executed"`
	NetworkRequests  int      `json:"network_requests"`
	Errors           int      `json:"errors"`
	ToolsUsed        []string `json:"tools_used,omitempty"`
}

type policyContextActionView struct {
	ID           string `json:"id"`
	Timestamp    string `json:"timestamp"`
	ActionType   string `json:"action_type"`
	Tool         string `json:"tool,omitempty"`
	Agent        string `json:"agent,omitempty"`
	ResultStatus string `json:"result_status"`
	DurationMS   *int64 `json:"duration_ms,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type policyContextSessionView struct {
	State   policyContextStateView    `json:"state"`
	Actions []policyContextActionView `json:"actions"`
}

func renderPolicyContextSession(ctx context.Context, w io.Writer, c *tui.Colorizer, store storage.Store, sessionRef string, limit int, format string) error {
	sessionID, err := resolveContextSessionID(ctx, store, sessionRef)
	if err != nil {
		return err
	}

	state, err := store.GetContextState(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load context state: %w", err)
	}

	actions, err := store.QueryContextActions(ctx, sessionID, limit)
	if err != nil {
		return fmt.Errorf("failed to load context actions: %w", err)
	}

	view := policyContextSessionView{Actions: make([]policyContextActionView, 0, len(actions))}
	if state != nil {
		view.State = stateRowToView(state)
	} else {
		view.State.SessionID = sessionID.String()
	}
	for _, a := range actions {
		view.Actions = append(view.Actions, actionRowToView(a))
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	renderContextStateTable(w, c, view.State)
	_, _ = fmt.Fprintln(w)
	renderContextActionsTable(w, c, view.Actions)
	return nil
}

func renderPolicyContextList(ctx context.Context, w io.Writer, c *tui.Colorizer, store storage.Store, limit int, format string) error {
	states, err := store.QueryAllContextStates(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to query context states: %w", err)
	}

	views := make([]policyContextStateView, 0, len(states))
	for _, s := range states {
		views = append(views, stateRowToView(s))
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(views)
	}

	renderContextStatesTable(w, c, views)
	return nil
}

func resolveContextSessionID(ctx context.Context, store storage.Store, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	state, err := store.GetContextStateByPrefix(ctx, ref)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve context state prefix: %w", err)
	}
	if state != nil {
		return state.SessionID, nil
	}
	sess, err := store.GetSessionByPrefix(ctx, ref)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve session prefix: %w", err)
	}
	if sess != nil {
		return sess.ID, nil
	}
	return uuid.Nil, fmt.Errorf("no session or context state matches prefix %q", ref)
}

func stateRowToView(s *storage.ContextStateRow) policyContextStateView {
	return policyContextStateView{
		SessionID:        s.SessionID.String(),
		FirstSeenAt:      s.FirstSeenAt.Format(time.RFC3339),
		LastActionAt:     s.LastActionAt.Format(time.RFC3339),
		TotalActions:     s.TotalActions,
		FilesRead:        s.FilesRead,
		FilesWritten:     s.FilesWritten,
		CommandsExecuted: s.CommandsExecuted,
		NetworkRequests:  s.NetworkRequests,
		Errors:           s.Errors,
		ToolsUsed:        s.ToolsUsed,
	}
}

func actionRowToView(a *storage.ContextActionRow) policyContextActionView {
	v := policyContextActionView{
		ID:           a.ID.String(),
		Timestamp:    a.Timestamp.Format(time.RFC3339),
		ActionType:   a.ActionType,
		Tool:         a.Tool,
		Agent:        a.Agent,
		ResultStatus: a.ResultStatus,
		ErrorMessage: a.ErrorMessage,
	}
	if a.DurationMS != nil {
		d := *a.DurationMS
		v.DurationMS = &d
	}
	return v
}

func renderContextStateTable(w io.Writer, c *tui.Colorizer, v policyContextStateView) {
	_, _ = fmt.Fprintln(w, c.Header("Context snapshot"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(80))
	_, _ = fmt.Fprintf(w, "  %-22s %s\n", c.Dim("session_id"), c.Cyan(v.SessionID))
	_, _ = fmt.Fprintf(w, "  %-22s %s\n", c.Dim("first_seen_at"), v.FirstSeenAt)
	_, _ = fmt.Fprintf(w, "  %-22s %s\n", c.Dim("last_action_at"), v.LastActionAt)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("total_actions"), v.TotalActions)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("files_read"), v.FilesRead)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("files_written"), v.FilesWritten)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("commands_executed"), v.CommandsExecuted)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("network_requests"), v.NetworkRequests)
	_, _ = fmt.Fprintf(w, "  %-22s %d\n", c.Dim("errors"), v.Errors)
	if len(v.ToolsUsed) > 0 {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", c.Dim("tools_used"), strings.Join(v.ToolsUsed, ", "))
	}
}

func renderContextActionsTable(w io.Writer, c *tui.Colorizer, actions []policyContextActionView) {
	if len(actions) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("No actions recorded for this session."))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Recent actions"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(80))
	_, _ = fmt.Fprintf(w, "  %-20s  %-16s  %-12s  %-9s  %s\n",
		c.Dim("timestamp"), c.Dim("action_type"), c.Dim("tool"), c.Dim("result"), c.Dim("id"))
	for _, a := range actions {
		_, _ = fmt.Fprintf(w, "  %-20s  %-16s  %-12s  %-9s  %s\n",
			a.Timestamp, a.ActionType, tui.TruncateString(a.Tool, 12), a.ResultStatus, tui.FormatShortID(a.ID))
	}
}

func renderContextStatesTable(w io.Writer, c *tui.Colorizer, states []policyContextStateView) {
	if len(states) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("No context state recorded yet."))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Context sessions"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(80))
	_, _ = fmt.Fprintf(w, "  %-13s  %-20s  %-7s  %-6s  %-6s  %-5s  %-6s  %s\n",
		c.Dim("session"), c.Dim("last_action_at"),
		c.Dim("actions"), c.Dim("reads"), c.Dim("writes"), c.Dim("cmds"),
		c.Dim("errors"), c.Dim("tools"))
	for _, s := range states {
		_, _ = fmt.Fprintf(w, "  %-13s  %-20s  %-7d  %-6d  %-6d  %-5d  %-6d  %d\n",
			tui.FormatShortID(s.SessionID), s.LastActionAt,
			s.TotalActions, s.FilesRead, s.FilesWritten, s.CommandsExecuted, s.Errors,
			len(s.ToolsUsed))
	}
}
