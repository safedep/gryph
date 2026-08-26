package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func newPolicyContextCmd() *cobra.Command {
	var (
		sessionID   string
		limit       int
		format      string
		verify      bool
		allSessions bool
	)

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Inspect AARM Context Accumulator state",
		Long: "Print the per-session counters captured by the Context Accumulator. " +
			"Without --session, lists every session that has a context state row, " +
			"ordered by last_action_at descending. With --session, prints the snapshot " +
			"for that session and the most recent action rows.\n\n" +
			"Pass --verify to re-derive the per-session hash chain on the action " +
			"rows and report any breaks. Rows written before the chain was added " +
			"have a NULL sequence and report as `unchained` rather than a chain " +
			"break. Verification scope:\n" +
			"  --verify --session ID     verifies the full chain for one session.\n" +
			"  --verify                  verifies sessions whose action rows " +
			"appear in the most recent --limit rows.\n" +
			"  --verify --all-sessions   enumerates every session in the context " +
			"log and verifies each chain in full.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if allSessions && sessionID != "" {
				return ErrConfig("invalid flags", fmt.Errorf("--all-sessions cannot be combined with --session"))
			}
			if allSessions && !verify {
				return ErrConfig("invalid flags", fmt.Errorf("--all-sessions requires --verify"))
			}

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

			if verify {
				return runPolicyContextVerify(ctx, out, c, app.Store, sessionID, limit, allSessions, format)
			}

			if sessionID != "" {
				return renderPolicyContextSession(ctx, out, c, app.Store, sessionID, limit, format)
			}
			return renderPolicyContextList(ctx, out, c, app.Store, limit, format)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to inspect")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of action rows or sessions to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().BoolVar(&verify, "verify", false, "re-derive the per-session hash chain and report any breaks")
	cmd.Flags().BoolVar(&allSessions, "all-sessions", false, "with --verify, enumerate every session in the context log and verify each chain in full. Mutually exclusive with --session")
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
	sessionID, err := resolveAarmSessionID(ctx, store, sessionRef)
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
	_, _ = fmt.Fprintf(w, "%s %s\n", c.Header("Session"), c.Cyan(tui.FormatShortID(v.SessionID)))
	_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("first seen"), v.FirstSeenAt)
	_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("last action"), v.LastActionAt)
	_, _ = fmt.Fprintf(w, "  %-12s %d\n", c.Dim("actions"), v.TotalActions)
	if v.FilesRead > 0 || v.FilesWritten > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %d read, %d written\n", c.Dim("files"), v.FilesRead, v.FilesWritten)
	}
	if v.CommandsExecuted > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %d\n", c.Dim("commands"), v.CommandsExecuted)
	}
	if v.NetworkRequests > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %d\n", c.Dim("network"), v.NetworkRequests)
	}
	if v.Errors > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %d\n", c.Dim("errors"), v.Errors)
	}
	if len(v.ToolsUsed) > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("tools"), strings.Join(v.ToolsUsed, ", "))
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

// contextVerifyBreak is the JSON-serializable break record surfaced by
// `gryph policy context --verify`. Mirrors receiptVerifyBreak.
type contextVerifyBreak struct {
	SessionID uuid.UUID `json:"session_id"`
	Sequence  int64     `json:"sequence"`
	Reason    string    `json:"reason"`
}

// contextVerifySummary aggregates the per-row verdicts into the three
// buckets reported by --verify: ok rows, broken rows, and unchained rows
// (pre-Phase-5a rows with NULL sequence/hash).
type contextVerifySummary struct {
	OK        int `json:"ok"`
	Broken    int `json:"broken"`
	Unchained int `json:"unchained"`
}

func runPolicyContextVerify(ctx context.Context, w io.Writer, c *tui.Colorizer, store storage.Store, sessionRef string, limit int, allSessions bool, format string) error {
	sessionIDs, err := collectContextVerifySessionIDs(ctx, store, sessionRef, limit, allSessions)
	if err != nil {
		return err
	}

	var (
		breaks   []contextVerifyBreak
		summary  contextVerifySummary
		allRows  []*storage.ContextActionRow
		sortRows = func(rows []*storage.ContextActionRow) {
			sort.SliceStable(rows, func(i, j int) bool {
				si, sj := rows[i].Sequence, rows[j].Sequence
				if si == nil && sj == nil {
					return rows[i].Timestamp.Before(rows[j].Timestamp)
				}
				if si == nil {
					return false
				}
				if sj == nil {
					return true
				}
				return *si < *sj
			})
		}
		collectAllRows = format == "json"
	)

	for _, sid := range sessionIDs {
		full, err := store.QueryContextActionsFiltered(ctx, &storage.ContextActionFilter{
			SessionID: &sid,
			Limit:     -1,
			Ascending: true,
		})
		if err != nil {
			return fmt.Errorf("verify: load session context: %w", err)
		}
		sortRows(full)

		chained := make([]contextchain.Row, 0, len(full))
		for _, r := range full {
			if r.Sequence == nil {
				summary.Unchained++
				continue
			}
			chained = append(chained, contextChainRowFromAction(r))
		}

		verified, sessionBreaks := contextchain.Verify(chained)
		summary.OK += verified
		summary.Broken += len(chained) - verified
		for _, b := range sessionBreaks {
			breaks = append(breaks, contextVerifyBreak{
				SessionID: b.SessionID,
				Sequence:  b.Sequence,
				Reason:    b.Reason,
			})
		}
		if collectAllRows {
			allRows = append(allRows, full...)
		}
	}

	if len(breaks) > 0 {
		emitContextChainBrokenAudit(ctx, store, breaks)
	}

	if format == "json" {
		if err := writeContextVerifyJSON(w, allRows, breaks, summary); err != nil {
			return err
		}
	} else {
		renderContextVerifyResults(w, c, summary, breaks)
	}

	if len(breaks) > 0 {
		return ErrConfig("context chain verification failed", fmt.Errorf("%d chain break(s)", len(breaks)))
	}
	return nil
}

func collectContextVerifySessionIDs(ctx context.Context, store storage.Store, sessionRef string, limit int, allSessions bool) ([]uuid.UUID, error) {
	if sessionRef != "" {
		sid, err := resolveAarmSessionID(ctx, store, sessionRef)
		if err != nil {
			return nil, err
		}
		return []uuid.UUID{sid}, nil
	}
	if allSessions {
		ids, err := store.ListContextSessionIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("verify: enumerate session IDs: %w", err)
		}
		return ids, nil
	}
	rows, err := store.QueryContextActionsFiltered(ctx, &storage.ContextActionFilter{Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("verify: collect recent sessions: %w", err)
	}
	seen := map[uuid.UUID]struct{}{}
	ids := make([]uuid.UUID, 0)
	for _, r := range rows {
		if _, ok := seen[r.SessionID]; ok {
			continue
		}
		seen[r.SessionID] = struct{}{}
		ids = append(ids, r.SessionID)
	}
	return ids, nil
}

func contextChainRowFromAction(r *storage.ContextActionRow) contextchain.Row {
	var seq int64
	if r.Sequence != nil {
		seq = *r.Sequence
	}
	var injection float32
	if r.InjectionScore != nil {
		injection = *r.InjectionScore
	}
	return contextchain.Row{
		SessionID: r.SessionID,
		Sequence:  seq,
		PrevHash:  r.PrevHash,
		Hash:      r.Hash,
		Fields: contextchain.InputFromRow(
			seq, r.PrevHash, r.Timestamp,
			r.SessionID, r.EventID, r.ID,
			r.ActionType, r.Tool, r.Agent, r.Project, r.WorkingDir,
			r.DataClassifications, injection,
		),
	}
}

func emitContextChainBrokenAudit(ctx context.Context, store storage.Store, breaks []contextVerifyBreak) {
	if store == nil || len(breaks) == 0 {
		return
	}
	details := map[string]interface{}{
		"break_count": len(breaks),
	}
	summary := make([]map[string]interface{}, 0, len(breaks))
	for _, b := range breaks {
		summary = append(summary, map[string]interface{}{
			"session_id": b.SessionID.String(),
			"sequence":   b.Sequence,
			"reason":     b.Reason,
		})
	}
	details["breaks"] = summary
	if err := logSelfAudit(ctx, store, SelfAuditActionContextChainBroken, "",
		details, SelfAuditResultError, "context chain verification failed"); err != nil {
		log.Errorf("failed to record context chain failure: %v", err)
	}
}

func renderContextVerifyResults(w io.Writer, c *tui.Colorizer, summary contextVerifySummary, breaks []contextVerifyBreak) {
	if len(breaks) == 0 {
		_, _ = fmt.Fprintln(w, c.Success("Context chain verification: OK"))
	} else {
		_, _ = fmt.Fprintln(w, c.Error("Context chain verification: FAILED"))
		for _, b := range breaks {
			_, _ = fmt.Fprintf(w, "  session=%s seq=%d %s\n",
				tui.FormatShortID(b.SessionID.String()), b.Sequence, b.Reason)
		}
	}
	_, _ = fmt.Fprintf(w, "  %s ok=%d broken=%d unchained=%d\n",
		c.Dim("summary"), summary.OK, len(breaks), summary.Unchained)
}

type contextVerifyActionView struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	Sequence   *int64 `json:"sequence,omitempty"`
	Timestamp  string `json:"timestamp"`
	ActionType string `json:"action_type"`
	Tool       string `json:"tool,omitempty"`
	Agent      string `json:"agent,omitempty"`
	PrevHash   string `json:"prev_hash,omitempty"`
	Hash       string `json:"hash,omitempty"`
}

func writeContextVerifyJSON(w io.Writer, rows []*storage.ContextActionRow, breaks []contextVerifyBreak, summary contextVerifySummary) error {
	views := make([]contextVerifyActionView, 0, len(rows))
	for _, r := range rows {
		v := contextVerifyActionView{
			ID:         r.ID.String(),
			SessionID:  r.SessionID.String(),
			Timestamp:  r.Timestamp.Format(time.RFC3339Nano),
			ActionType: r.ActionType,
			Tool:       r.Tool,
			Agent:      r.Agent,
		}
		if r.Sequence != nil {
			s := *r.Sequence
			v.Sequence = &s
		}
		if len(r.PrevHash) > 0 {
			v.PrevHash = hex.EncodeToString(r.PrevHash)
		}
		if len(r.Hash) > 0 {
			v.Hash = hex.EncodeToString(r.Hash)
		}
		views = append(views, v)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := map[string]interface{}{
		"actions":      views,
		"chain_breaks": breaks,
		"summary":      summary,
	}
	return enc.Encode(out)
}
