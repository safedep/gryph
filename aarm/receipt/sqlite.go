package receipt

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage"
)

// SQLiteGenerator is the SQLite-backed Generator. It owns the read-last
// (sequence, hash) -> compute hash -> insert flow. SQLite's writer lock
// (WAL mode) serializes concurrent same-session writes; the
// (session_id, sequence) unique index is the crash-recovery rail that
// surfaces any racing duplicate as an insert error.
type SQLiteGenerator struct {
	store  storage.ReceiptStore
	now    func() time.Time
	signer Signer
}

// NewSQLite returns a SQLite-backed Generator. The store must be non-nil.
func NewSQLite(store storage.ReceiptStore, opts ...GeneratorOption) *SQLiteGenerator {
	cfg := &generatorConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &SQLiteGenerator{
		store:  store,
		now:    func() time.Time { return time.Now().UTC() },
		signer: cfg.signer,
	}
}

var _ Generator = (*SQLiteGenerator)(nil)

// Record implements Generator. It defers the (read last sequence -> compute
// hash -> insert) flow to ReceiptStore.RecordReceiptInTx so all three steps
// run inside one writer transaction. Errors are wrapped with ErrInsert (and
// joined with the underlying cause) so the lazy policy wrapper can surface
// them in self-audit.
func (g *SQLiteGenerator) Record(ctx context.Context, in *RecordInput) (*Record, error) {
	if g == nil || g.store == nil {
		return nil, errors.Join(ErrInsert, fmt.Errorf("store is not initialized"))
	}
	if in == nil {
		return nil, errors.Join(ErrInsert, fmt.Errorf("nil input"))
	}
	if in.SessionID == uuid.Nil {
		return nil, errors.Join(ErrInsert, fmt.Errorf("nil session ID"))
	}

	recordedAt := in.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = g.now()
	}

	row, err := g.store.RecordReceiptInTx(ctx, in.SessionID, func(prev *storage.ReceiptRow) (*storage.ReceiptRow, error) {
		var (
			nextSeq  int64 = 1
			prevHash []byte
		)
		if prev != nil {
			nextSeq = prev.Sequence + 1
			prevHash = prev.Hash
		}

		next := &storage.ReceiptRow{
			ID:         uuid.New(),
			SessionID:  in.SessionID,
			ActionID:   in.ActionID,
			EventID:    in.EventID,
			RecordedAt: recordedAt,
			Sequence:   nextSeq,
			PrevHash:   prevHash,
		}

		if in.Action != nil {
			next.Agent = in.Action.Agent
			next.Tool = in.Action.Tool
			next.ActionType = string(in.Action.Type)
			next.Project = in.Action.Project
			next.ActionPayload = actionPayloadMap(in.Action)
			next.SubagentID = in.Action.SubagentID
			next.SubagentType = in.Action.SubagentType
			next.HumanPrincipal = in.Action.HumanPrincipal
			next.ServiceIdentity = in.Action.ServiceIdentity
			next.RoleScope = in.Action.RoleScope
		}
		if next.Agent == "" {
			next.Agent = in.Agent
		}

		if in.Decision != nil {
			next.Decision = string(in.Decision.Decision)
			next.MatchedRuleIDs = in.Decision.MatchedRuleIDs
			next.Severity = string(in.Decision.Severity)
			next.Message = in.Decision.Message
		}

		if in.Snapshot != nil {
			next.Snapshot = snapshotMap(in.Snapshot)
		}

		next.PolicyHash = in.PolicyHash
		next.DeferReason = in.DeferReason
		next.ErrorMessage = in.ErrorMessage
		if in.DeferralOfSequence != nil {
			v := *in.DeferralOfSequence
			next.DeferralOfSequence = &v
		}
		next.ResultStatus = DeriveInsertResultStatus(next.Decision)
		next.HashVersion = HashV2
		if hasContent(next.ActionPayload) {
			salt := in.ContentSalt
			if len(salt) == 0 {
				var err error
				if salt, err = newContentSalt(); err != nil {
					return nil, err
				}
			}
			command, url, err := contentDigests(salt, next.ActionPayload)
			if err != nil {
				return nil, err
			}
			next.ContentSalt, next.CommandDigest, next.URLDigest = salt, command, url
		}

		hash, err := ComputeHash(NewHashInput(hashFieldsFromRow(next)))
		if err != nil {
			return nil, fmt.Errorf("compute hash: %w", err)
		}
		next.Hash = hash

		if g.signer != nil {
			sig, keyID, signErr := g.signer.Sign(hash)
			if signErr != nil {
				return nil, fmt.Errorf("sign receipt hash: %w", signErr)
			}
			next.Signature = sig
			next.SignerKeyID = keyID
		}
		return next, nil
	})
	if err != nil {
		return nil, errors.Join(ErrInsert, err)
	}

	return &Record{
		ID:          row.ID,
		Sequence:    row.Sequence,
		RecordedAt:  row.RecordedAt,
		Hash:        row.Hash,
		PrevHash:    row.PrevHash,
		SignerKeyID: row.SignerKeyID,
	}, nil
}

// UpdateResult implements Generator.
func (g *SQLiteGenerator) UpdateResult(ctx context.Context, sessionID uuid.UUID, sequence int64, result model.Result) error {
	if g == nil || g.store == nil {
		return fmt.Errorf("receipt: store is not initialized")
	}
	status := string(result.Status)
	if status == "" {
		status = string(model.ResultSuccess)
	}
	return g.store.UpdateReceiptResult(ctx, sessionID, sequence, status, result.Duration.Milliseconds(), result.Error)
}

// UpdateDecision implements Generator.
func (g *SQLiteGenerator) UpdateDecision(ctx context.Context, sessionID uuid.UUID, sequence int64, decision string, resultStatus string, note string) error {
	if g == nil || g.store == nil {
		return fmt.Errorf("receipt: store is not initialized")
	}
	return g.store.UpdateReceiptDecision(ctx, sessionID, sequence, decision, resultStatus, note)
}

// snapshotMap copies snapshot fields into the JSON-friendly map persisted on
// the receipt row. The receipt hash covers the map, and the verifier reads
// the stored map, so a key change affects new receipts only. The map holds
// counts for the entities, the egress hosts and the entries, because they
// hold paths, commands and hosts, and a hash-chained receipt can never drop
// them.
func snapshotMap(s *model.ContextSnapshot) map[string]interface{} {
	if s == nil {
		return nil
	}
	m := map[string]interface{}{
		"total_actions":     s.TotalActions,
		"files_read":        s.FilesRead,
		"files_written":     s.FilesWritten,
		"commands_executed": s.CommandsExecuted,
		"network_requests":  s.NetworkRequests,
		"errors":            s.Errors,
		"session_duration":  int64(s.SessionDuration),
		"entities_seen":     len(s.EntitiesSeen),
		"egress_hosts":      len(s.EgressHosts),
		"entries":           len(s.Entries),
	}
	if len(s.ToolsUsed) > 0 {
		m["tools_used"] = append([]string(nil), s.ToolsUsed...)
	}
	if len(s.ClassificationsSeen) > 0 {
		m["classifications_seen"] = append([]string(nil), s.ClassificationsSeen...)
	}
	if len(s.TagsSeen) > 0 {
		m["tags_seen"] = slices.Sorted(maps.Keys(s.TagsSeen))
	}
	if len(s.OriginsSeen) > 0 {
		m["origins_seen"] = append([]string(nil), s.OriginsSeen...)
	}
	return m
}

// actionPayloadMap copies normalized action parameters into a JSON-friendly
// map suitable for the receipt row.
func actionPayloadMap(a *model.Action) map[string]interface{} {
	if a == nil {
		return nil
	}
	m := map[string]interface{}{}
	if a.Operation != "" {
		m["operation"] = a.Operation
	}
	if a.Parameters.Path != "" {
		m["path"] = a.Parameters.Path
	}
	if a.Parameters.Command != "" {
		m["command"] = a.Parameters.Command
	}
	if len(a.Parameters.Args) > 0 {
		m["args"] = append([]string(nil), a.Parameters.Args...)
	}
	if a.Parameters.URL != "" {
		m["url"] = a.Parameters.URL
	}
	if a.Parameters.SizeBytes > 0 {
		m["size_bytes"] = a.Parameters.SizeBytes
	}
	if a.Parameters.LinesAdded > 0 {
		m["lines_added"] = a.Parameters.LinesAdded
	}
	if a.Parameters.LinesRemoved > 0 {
		m["lines_removed"] = a.Parameters.LinesRemoved
	}
	if a.WorkingDir != "" {
		m["working_dir"] = a.WorkingDir
	}
	if a.Phase != "" && a.Phase != model.PhaseUnknown {
		m["phase"] = string(a.Phase)
	}
	if a.ContentTruncated {
		m["content_truncated"] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
