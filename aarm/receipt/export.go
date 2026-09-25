package receipt

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/storage"
)

// ExportOptions controls Receipt export filtering and emission shape.
type ExportOptions struct {
	SessionID         *uuid.UUID
	Since             *time.Time
	Until             *time.Time
	Format            string
	IncludeSignatures bool
	// BatchSize controls how many rows are loaded into memory at once. Zero
	// or negative falls back to exportDefaultBatchSize. The exporter pages
	// through the result set in batches of this size so a huge export does
	// not load every row up front.
	BatchSize int
	// Profile, when set, applies to the command and the URL in the action
	// payload of a JSONL row. The digest columns never change.
	Profile *privacy.ExportProfile
	// Events loads the audit event of each receipt, so Profile can read the
	// labels of its content. A receipt without an audit event gets the
	// treatment of an unknown_sensitive value.
	Events EventLoader
	// Stats, when set, receives counts of rows that need the attention of
	// the caller.
	Stats *ExportStats
}

// ExportStats counts the rows that an export profile could not fully
// protect or that cannot verify after the export.
type ExportStats struct {
	// ProjectedV1 counts v1 rows whose content the profile removed. The v1
	// hash covers the content, so these rows fail verification.
	ProjectedV1 int
	// MessageQuotes counts rows whose rule message quotes content that the
	// profile removed. The hash covers the message, so the export keeps it.
	MessageQuotes int
}

// EventLoader loads audit events by ID.
type EventLoader interface {
	QueryEventsByIDs(ctx context.Context, ids []uuid.UUID) ([]*events.Event, error)
}

const (
	// ExportFormatJSONL is one full receipt JSON object per line.
	ExportFormatJSONL = "jsonl"
	// ExportFormatCSV is a flat CSV row per receipt. snapshot and
	// action_payload columns are omitted; non-scalar fields are emitted as
	// their canonical compact JSON.
	ExportFormatCSV = "csv"

	exportDefaultBatchSize = 500
)

// Exporter streams receipts to w in the configured format.
type Exporter interface {
	Export(ctx context.Context, w io.Writer, opts ExportOptions) error
}

// SQLiteExporter pulls receipts from a storage.ReceiptStore via paged
// timestamp scans so large exports do not buffer the full result set into
// memory.
type SQLiteExporter struct {
	store storage.ReceiptStore
}

// NewSQLiteExporter returns an Exporter backed by store.
func NewSQLiteExporter(store storage.ReceiptStore) *SQLiteExporter {
	return &SQLiteExporter{store: store}
}

var _ Exporter = (*SQLiteExporter)(nil)

// Export implements Exporter. Format defaults to jsonl.
func (e *SQLiteExporter) Export(ctx context.Context, w io.Writer, opts ExportOptions) error {
	if e == nil || e.store == nil {
		return fmt.Errorf("receipt: exporter is not initialized")
	}
	format := strings.ToLower(opts.Format)
	if format == "" {
		format = ExportFormatJSONL
	}

	sink, finishFn, err := exporterSink(w, format, opts.IncludeSignatures)
	if err != nil {
		return err
	}
	emit := func(rows []*storage.ReceiptRow) error {
		treatments, err := rowTreatments(ctx, rows, opts)
		if err != nil {
			return err
		}
		for _, r := range rows {
			exp := ToExported(r, opts.IncludeSignatures)
			if t, ok := treatments[r.ID]; ok {
				projectRow(&exp, r, t, opts.Stats)
			}
			shown := *r
			shown.ErrorMessage = exp.ErrorMessage
			if err := sink(&shown, exp); err != nil {
				return err
			}
		}
		return nil
	}

	if opts.SessionID != nil {
		full, err := e.store.QueryReceipts(ctx, &storage.ReceiptFilter{
			SessionID: opts.SessionID,
			Since:     opts.Since,
			Until:     opts.Until,
			Limit:     -1,
		})
		if err != nil {
			return fmt.Errorf("receipt: load session receipts for export: %w", err)
		}
		for batch := range slices.Chunk(full, exportDefaultBatchSize) {
			if err := emit(batch); err != nil {
				return err
			}
		}
		return finishFn()
	}

	batch := opts.BatchSize
	if batch <= 0 {
		batch = exportDefaultBatchSize
	}

	var (
		cursorTime *time.Time
		cursorID   *uuid.UUID
	)
	for {
		filter := &storage.ReceiptFilter{
			Since: opts.Since,
			Until: opts.Until,
			Limit: batch,
		}
		if cursorTime != nil {
			filter.UntilExclusive = cursorTime
			filter.UntilID = cursorID
		}
		rows, err := e.store.QueryReceipts(ctx, filter)
		if err != nil {
			return fmt.Errorf("receipt: load receipts for export: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		if err := emit(rows); err != nil {
			return err
		}
		last := rows[len(rows)-1]
		nextTime := last.RecordedAt
		nextID := last.ID
		cursorTime = &nextTime
		cursorID = &nextID
	}

	return finishFn()
}

// ExportedReceipt is the JSONL row shape. Snapshot and ActionPayload preserve
// their raw map representation so downstream consumers can re-derive the hash.
type ExportedReceipt struct {
	ID                 string                 `json:"id"`
	SessionID          string                 `json:"session_id"`
	ActionID           string                 `json:"action_id,omitempty"`
	EventID            string                 `json:"event_id,omitempty"`
	Sequence           int64                  `json:"sequence"`
	RecordedAt         string                 `json:"recorded_at"`
	RecordedAtUnix     int64                  `json:"recorded_at_unix_ns"`
	Agent              string                 `json:"agent,omitempty"`
	Tool               string                 `json:"tool,omitempty"`
	ActionType         string                 `json:"action_type"`
	Project            string                 `json:"project,omitempty"`
	Decision           string                 `json:"decision"`
	MatchedRuleIDs     []string               `json:"matched_rule_ids,omitempty"`
	Severity           string                 `json:"severity,omitempty"`
	Message            string                 `json:"message,omitempty"`
	ResultStatus       string                 `json:"result_status"`
	DurationMS         *int64                 `json:"duration_ms,omitempty"`
	ErrorMessage       string                 `json:"error_message,omitempty"`
	Snapshot           map[string]interface{} `json:"snapshot,omitempty"`
	ActionPayload      map[string]interface{} `json:"action_payload,omitempty"`
	PrevHash           string                 `json:"prev_hash,omitempty"`
	Hash               string                 `json:"hash"`
	SubagentID         string                 `json:"subagent_id,omitempty"`
	SubagentType       string                 `json:"subagent_type,omitempty"`
	PolicyHash         string                 `json:"policy_hash,omitempty"`
	SignerKeyID        string                 `json:"signer_key_id,omitempty"`
	Signature          string                 `json:"signature,omitempty"`
	DeferReason        string                 `json:"defer_reason,omitempty"`
	DeferralOfSequence *int64                 `json:"deferral_of_sequence,omitempty"`
	HumanPrincipal     string                 `json:"human_principal,omitempty"`
	ServiceIdentity    string                 `json:"service_identity,omitempty"`
	RoleScope          string                 `json:"role_scope,omitempty"`
	CommandDigest      string                 `json:"command_digest,omitempty"`
	URLDigest          string                 `json:"url_digest,omitempty"`
	HashVersion        int                    `json:"hash_version,omitempty"`
	ContentSalt        string                 `json:"content_salt,omitempty"`
	// Projected is true when an export profile changed the command or the
	// URL in ActionPayload. A v1 hash covers them, so such a v1 row cannot
	// verify.
	Projected bool `json:"projected,omitempty"`
}

// ToExported converts a storage.ReceiptRow into the export shape. When
// includeSig is false the Signature / SignerKeyID fields are zeroed.
func ToExported(r *storage.ReceiptRow, includeSig bool) ExportedReceipt {
	out := ExportedReceipt{
		ID:             r.ID.String(),
		SessionID:      r.SessionID.String(),
		Sequence:       r.Sequence,
		RecordedAt:     r.RecordedAt.Format(time.RFC3339Nano),
		RecordedAtUnix: r.RecordedAt.UnixNano(),
		Agent:          r.Agent,
		Tool:           r.Tool,
		ActionType:     r.ActionType,
		Project:        r.Project,
		Decision:       r.Decision,
		MatchedRuleIDs: r.MatchedRuleIDs,
		Severity:       r.Severity,
		Message:        r.Message,
		ResultStatus:   r.ResultStatus,
		DurationMS:     r.DurationMS,
		ErrorMessage:   r.ErrorMessage,
		Snapshot:       r.Snapshot,
		ActionPayload:  r.ActionPayload,
		Hash:           hex.EncodeToString(r.Hash),
		SubagentID:     r.SubagentID,
		SubagentType:   r.SubagentType,
	}
	if r.ActionID != uuid.Nil {
		out.ActionID = r.ActionID.String()
	}
	if r.EventID != uuid.Nil {
		out.EventID = r.EventID.String()
	}
	if len(r.PrevHash) > 0 {
		out.PrevHash = hex.EncodeToString(r.PrevHash)
	}
	if len(r.PolicyHash) > 0 {
		out.PolicyHash = hex.EncodeToString(r.PolicyHash)
	}
	if includeSig {
		out.SignerKeyID = r.SignerKeyID
		if len(r.Signature) > 0 {
			out.Signature = base64.StdEncoding.EncodeToString(r.Signature)
		}
	}
	if r.DeferReason != "" {
		out.DeferReason = r.DeferReason
	}
	if r.DeferralOfSequence != nil {
		v := *r.DeferralOfSequence
		out.DeferralOfSequence = &v
	}
	out.HumanPrincipal = r.HumanPrincipal
	out.ServiceIdentity = r.ServiceIdentity
	out.RoleScope = r.RoleScope
	out.CommandDigest = r.CommandDigest
	out.URLDigest = r.URLDigest
	out.HashVersion = r.HashVersion
	if len(r.ContentSalt) > 0 {
		out.ContentSalt = hex.EncodeToString(r.ContentSalt)
	}
	return out
}

// rowTreatments returns the treatment of the content of each row, when
// opts has a profile. The content of a row is plain text relative to its
// audit event, so it gets Event.PlainTreatment.
func rowTreatments(ctx context.Context, rows []*storage.ReceiptRow, opts ExportOptions) (map[uuid.UUID]privacy.Treatment, error) {
	if opts.Profile == nil {
		return nil, nil
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, r := range rows {
		if r.EventID != uuid.Nil && !seen[r.EventID] {
			seen[r.EventID] = true
			ids = append(ids, r.EventID)
		}
	}
	byID := map[uuid.UUID]*events.Event{}
	if opts.Events != nil {
		for chunk := range slices.Chunk(ids, exportDefaultBatchSize) {
			evts, err := opts.Events.QueryEventsByIDs(ctx, chunk)
			if err != nil {
				return nil, fmt.Errorf("receipt: load audit events for export: %w", err)
			}
			for _, e := range evts {
				byID[e.ID] = e
			}
		}
	}
	unknown := opts.Profile.Treatment(privacy.Label{Origin: privacy.OriginAgent, Classes: []privacy.Class{privacy.ClassUnknownSensitive}})
	out := make(map[uuid.UUID]privacy.Treatment, len(rows))
	for _, r := range rows {
		out[r.ID] = unknown
		if e, ok := byID[r.EventID]; ok {
			out[r.ID] = e.PlainTreatment(*opts.Profile)
		}
	}
	return out, nil
}

// projectRow applies the treatment to the content keys of the payload and
// to the error message of an exported row. The row loses its content salt
// with the content, so the commitments cannot be reversed.
func projectRow(exp *ExportedReceipt, r *storage.ReceiptRow, t privacy.Treatment, stats *ExportStats) {
	if t == privacy.TreatInclude {
		return
	}
	exp.ErrorMessage = t.Plain(r.ErrorMessage)
	removed := contentStrings(r.ActionPayload)
	if len(removed) == 0 {
		return
	}
	payload := maps.Clone(r.ActionPayload)
	for _, key := range contentKeys {
		if _, ok := payload[key]; !ok {
			continue
		}
		if t == privacy.TreatRedact && key != payloadKeyArgs {
			payload[key] = privacy.RedactedValue
		} else {
			delete(payload, key)
		}
	}
	exp.ActionPayload = payload
	exp.ContentSalt = ""
	exp.Projected = true
	if stats == nil {
		return
	}
	if r.HashVersion != HashV2 {
		stats.ProjectedV1++
	}
	if slices.ContainsFunc(removed, func(v string) bool { return strings.Contains(r.Message, v) }) {
		stats.MessageQuotes++
	}
}

// contentStrings returns the non-empty string values under the content keys.
func contentStrings(payload map[string]interface{}) []string {
	var out []string
	add := func(v any) {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	for _, key := range contentKeys {
		switch v := payload[key].(type) {
		case []string:
			for _, s := range v {
				add(s)
			}
		case []interface{}:
			for _, s := range v {
				add(s)
			}
		default:
			add(v)
		}
	}
	return out
}

func exporterSink(w io.Writer, format string, includeSig bool) (func(*storage.ReceiptRow, ExportedReceipt) error, func() error, error) {
	switch format {
	case ExportFormatJSONL:
		enc := json.NewEncoder(w)
		return func(_ *storage.ReceiptRow, exp ExportedReceipt) error {
			return enc.Encode(exp)
		}, func() error { return nil }, nil
	case ExportFormatCSV:
		cw := csv.NewWriter(w)
		headers := csvHeaders(includeSig)
		if err := cw.Write(headers); err != nil {
			return nil, nil, err
		}
		return func(r *storage.ReceiptRow, _ ExportedReceipt) error {
				return cw.Write(csvRow(r, includeSig))
			}, func() error {
				cw.Flush()
				return cw.Error()
			}, nil
	default:
		return nil, nil, fmt.Errorf("receipt: unknown export format %q", format)
	}
}

func csvHeaders(includeSig bool) []string {
	h := []string{
		"id", "session_id", "action_id", "event_id", "sequence",
		"recorded_at", "recorded_at_unix_ns",
		"agent", "tool", "action_type", "project",
		"decision", "matched_rule_ids", "severity", "message",
		"result_status", "duration_ms", "error_message",
		"prev_hash", "hash",
		"subagent_id", "subagent_type", "policy_hash",
		"defer_reason", "deferral_of_sequence",
		"human_principal", "service_identity", "role_scope",
		"command_digest", "url_digest", "hash_version",
	}
	if includeSig {
		h = append(h, "signature", "signer_key_id")
	}
	return h
}

func csvRow(r *storage.ReceiptRow, includeSig bool) []string {
	actionID := ""
	if r.ActionID != uuid.Nil {
		actionID = r.ActionID.String()
	}
	eventID := ""
	if r.EventID != uuid.Nil {
		eventID = r.EventID.String()
	}
	matched := ""
	if len(r.MatchedRuleIDs) > 0 {
		b, _ := json.Marshal(r.MatchedRuleIDs)
		matched = string(b)
	}
	duration := ""
	if r.DurationMS != nil {
		duration = strconv.FormatInt(*r.DurationMS, 10)
	}
	deferralOfSequence := ""
	if r.DeferralOfSequence != nil {
		deferralOfSequence = strconv.FormatInt(*r.DeferralOfSequence, 10)
	}
	row := []string{
		r.ID.String(),
		r.SessionID.String(),
		actionID,
		eventID,
		strconv.FormatInt(r.Sequence, 10),
		r.RecordedAt.Format(time.RFC3339Nano),
		strconv.FormatInt(r.RecordedAt.UnixNano(), 10),
		r.Agent,
		r.Tool,
		r.ActionType,
		r.Project,
		r.Decision,
		matched,
		r.Severity,
		r.Message,
		r.ResultStatus,
		duration,
		r.ErrorMessage,
		hex.EncodeToString(r.PrevHash),
		hex.EncodeToString(r.Hash),
		r.SubagentID,
		r.SubagentType,
		hex.EncodeToString(r.PolicyHash),
		r.DeferReason,
		deferralOfSequence,
		r.HumanPrincipal,
		r.ServiceIdentity,
		r.RoleScope,
		r.CommandDigest,
		r.URLDigest,
		strconv.Itoa(r.HashVersion),
	}
	if includeSig {
		row = append(row, base64.StdEncoding.EncodeToString(r.Signature), r.SignerKeyID)
	}
	return row
}
