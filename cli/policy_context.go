package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
		Short: "Inspect the session context",
		Long: "Print the session context: the session counters and the state that " +
			"the Context Accumulator keeps. Without --session, lists every session " +
			"that has a context state row, newest first. With --session, prints the " +
			"state of that session and its most recent entries.\n\n" +
			"Pass --verify to re-derive the per-session hash chain on the entries " +
			"and report any breaks. Verification scope:\n" +
			"  --verify --session ID     verifies the full chain for one session.\n" +
			"  --verify                  verifies sessions whose entries " +
			"appear in the most recent --limit entries.\n" +
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
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of entries or sessions to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().BoolVar(&verify, "verify", false, "re-derive the per-session hash chain and report any breaks")
	cmd.Flags().BoolVar(&allSessions, "all-sessions", false, "with --verify, enumerate every session in the context log and verify each chain in full. Mutually exclusive with --session")
	return cmd
}

type policyContextStateView struct {
	SessionID           string   `json:"session_id"`
	StartedAt           string   `json:"started_at,omitempty"`
	LastEntryAt         string   `json:"last_entry_at,omitempty"`
	TotalActions        int      `json:"total_actions"`
	FilesRead           int      `json:"files_read"`
	FilesWritten        int      `json:"files_written"`
	CommandsExecuted    int      `json:"commands_executed"`
	NetworkRequests     int      `json:"network_requests"`
	Errors              int      `json:"errors"`
	ToolsUsed           []string `json:"tools_used,omitempty"`
	ClassificationsSeen []string `json:"classifications_seen,omitempty"`
	IntentAvailable     bool     `json:"intent_available"`
	ActionsSinceIntent  int      `json:"actions_since_intent"`
}

type policyContextEntryView struct {
	ID           string   `json:"id"`
	Sequence     int64    `json:"sequence"`
	Kind         string   `json:"kind"`
	Timestamp    string   `json:"timestamp"`
	ActionType   string   `json:"action_type"`
	Tool         string   `json:"tool,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	MatchedRules []string `json:"matched_rule_ids,omitempty"`
	ResultStatus string   `json:"result_status"`
	DurationMS   *int64   `json:"duration_ms,omitempty"`
	ErrorMessage string   `json:"error_message,omitempty"`
}

type policyContextSessionView struct {
	State   policyContextStateView   `json:"state"`
	Entries []policyContextEntryView `json:"entries"`
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

	entries, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sessionID, Limit: limit})
	if err != nil {
		return fmt.Errorf("failed to load context entries: %w", err)
	}

	view := policyContextSessionView{Entries: make([]policyContextEntryView, 0, len(entries))}
	if state != nil {
		view.State = stateRowToView(state)
	} else {
		view.State.SessionID = sessionID.String()
	}
	for _, e := range entries {
		view.Entries = append(view.Entries, entryRowToView(e))
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	renderContextStateTable(w, c, view.State)
	_, _ = fmt.Fprintln(w)
	renderContextEntriesTable(w, c, view.Entries)
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

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func stateRowToView(s *storage.ContextStateRow) policyContextStateView {
	return policyContextStateView{
		SessionID:           s.SessionID.String(),
		StartedAt:           formatTime(s.StartedAt),
		LastEntryAt:         formatTime(s.LastEntryAt),
		TotalActions:        s.TotalActions,
		FilesRead:           s.FilesRead,
		FilesWritten:        s.FilesWritten,
		CommandsExecuted:    s.CommandsExecuted,
		NetworkRequests:     s.NetworkRequests,
		Errors:              s.Errors,
		ToolsUsed:           s.ToolsUsed,
		ClassificationsSeen: s.ClassificationsSeen,
		IntentAvailable:     s.LastIntentSeq != nil,
		ActionsSinceIntent:  s.ActionsSinceIntent,
	}
}

func entryRowToView(e *storage.ContextEntryRow) policyContextEntryView {
	return policyContextEntryView{
		ID:           e.ID.String(),
		Sequence:     e.Sequence,
		Kind:         e.Kind,
		Timestamp:    e.Timestamp.Format(time.RFC3339),
		ActionType:   e.ActionType,
		Tool:         e.Tool,
		Decision:     e.Decision,
		MatchedRules: e.MatchedRuleIDs,
		ResultStatus: e.ResultStatus,
		DurationMS:   e.DurationMS,
		ErrorMessage: e.ErrorMessage,
	}
}

func renderContextStateTable(w io.Writer, c *tui.Colorizer, v policyContextStateView) {
	_, _ = fmt.Fprintf(w, "%s %s\n", c.Header("Session"), c.Cyan(tui.FormatShortID(v.SessionID)))
	_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("started"), v.StartedAt)
	_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("last entry"), v.LastEntryAt)
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
	if v.IntentAvailable {
		_, _ = fmt.Fprintf(w, "  %-12s %d actions since the last prompt\n", c.Dim("intent"), v.ActionsSinceIntent)
	} else {
		_, _ = fmt.Fprintf(w, "  %-12s %s\n", c.Dim("intent"), "not available")
	}
}

func renderContextEntriesTable(w io.Writer, c *tui.Colorizer, entries []policyContextEntryView) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("No entries recorded for this session."))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Recent entries"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(80))
	_, _ = fmt.Fprintf(w, "  %-5s  %-11s  %-14s  %-12s  %-8s  %-9s  %s\n",
		c.Dim("seq"), c.Dim("kind"), c.Dim("action_type"), c.Dim("tool"), c.Dim("decision"), c.Dim("result"), c.Dim("id"))
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "  %-5d  %-11s  %-14s  %-12s  %-8s  %-9s  %s\n",
			e.Sequence, e.Kind, e.ActionType, tui.TruncateString(e.Tool, 12), e.Decision, e.ResultStatus, tui.FormatShortID(e.ID))
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
		c.Dim("session"), c.Dim("last_entry_at"),
		c.Dim("actions"), c.Dim("reads"), c.Dim("writes"), c.Dim("cmds"),
		c.Dim("errors"), c.Dim("tools"))
	for _, s := range states {
		_, _ = fmt.Fprintf(w, "  %-13s  %-20s  %-7d  %-6d  %-6d  %-5d  %-6d  %d\n",
			tui.FormatShortID(s.SessionID), s.LastEntryAt,
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

// contextVerifySummary counts the entries that verified and the entries
// with at least one break.
type contextVerifySummary struct {
	OK     int `json:"ok"`
	Broken int `json:"broken"`
}

func runPolicyContextVerify(ctx context.Context, w io.Writer, c *tui.Colorizer, store storage.Store, sessionRef string, limit int, allSessions bool, format string) error {
	sessionIDs, err := collectContextVerifySessionIDs(ctx, store, sessionRef, limit, allSessions)
	if err != nil {
		return err
	}

	var (
		breaks         []contextVerifyBreak
		summary        contextVerifySummary
		allRows        []*storage.ContextEntryRow
		collectAllRows = format == "json"
	)

	for _, sid := range sessionIDs {
		full, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{
			SessionID: &sid,
			Limit:     -1,
			Ascending: true,
		})
		if err != nil {
			return fmt.Errorf("verify: load session context: %w", err)
		}

		chained := make([]contextchain.Row, 0, len(full))
		for _, r := range full {
			chained = append(chained, contextchain.Row{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Version:   r.HashVersion,
				PrevHash:  r.PrevHash,
				Hash:      r.Hash,
				Fields:    storage.ContextChainInput(r),
			})
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
	rows, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{Limit: limit})
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
	_, _ = fmt.Fprintf(w, "  %s ok=%d broken=%d\n",
		c.Dim("summary"), summary.OK, summary.Broken)
}

type contextVerifyEntryView struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	Sequence    int64  `json:"sequence"`
	Kind        string `json:"kind"`
	Timestamp   string `json:"timestamp"`
	ActionType  string `json:"action_type"`
	Tool        string `json:"tool,omitempty"`
	HashVersion int    `json:"hash_version"`
	PrevHash    string `json:"prev_hash,omitempty"`
	Hash        string `json:"hash,omitempty"`
}

func writeContextVerifyJSON(w io.Writer, rows []*storage.ContextEntryRow, breaks []contextVerifyBreak, summary contextVerifySummary) error {
	views := make([]contextVerifyEntryView, 0, len(rows))
	for _, r := range rows {
		views = append(views, contextVerifyEntryView{
			ID:          r.ID.String(),
			SessionID:   r.SessionID.String(),
			Sequence:    r.Sequence,
			Kind:        r.Kind,
			Timestamp:   r.Timestamp.Format(time.RFC3339Nano),
			ActionType:  r.ActionType,
			Tool:        r.Tool,
			HashVersion: r.HashVersion,
			PrevHash:    hex.EncodeToString(r.PrevHash),
			Hash:        hex.EncodeToString(r.Hash),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := map[string]interface{}{
		"entries":      views,
		"chain_breaks": breaks,
		"summary":      summary,
	}
	return enc.Encode(out)
}
