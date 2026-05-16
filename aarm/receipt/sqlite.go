package receipt

import (
	"context"
	"errors"
	"fmt"
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

		hashFields := HashInputFields{
			Sequence:        next.Sequence,
			PrevHash:        next.PrevHash,
			RecordedAtUnix:  next.RecordedAt.UnixNano(),
			SessionID:       next.SessionID,
			ActionID:        next.ActionID,
			EventID:         next.EventID,
			Agent:           next.Agent,
			Tool:            next.Tool,
			ActionType:      next.ActionType,
			Project:         next.Project,
			Decision:        next.Decision,
			Severity:        next.Severity,
			Message:         next.Message,
			MatchedRuleIDs:  next.MatchedRuleIDs,
			Snapshot:        next.Snapshot,
			ActionPayload:   next.ActionPayload,
			SubagentID:      next.SubagentID,
			SubagentType:    next.SubagentType,
			PolicyHash:      next.PolicyHash,
			DeferReason:     next.DeferReason,
			HumanPrincipal:  next.HumanPrincipal,
			ServiceIdentity: next.ServiceIdentity,
			RoleScope:       next.RoleScope,
		}
		if next.DeferralOfSequence != nil {
			hashFields.DeferralOfSequence = *next.DeferralOfSequence
		}
		hashInput := NewHashInput(hashFields)
		hash, err := ComputeHash(hashInput)
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
// the receipt row.
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
		"semantic_drift":    s.SemanticDrift,
	}
	if len(s.ToolsUsed) > 0 {
		m["tools_used"] = append([]string(nil), s.ToolsUsed...)
	}
	if len(s.ClassificationsSeen) > 0 {
		m["classifications_seen"] = append([]string(nil), s.ClassificationsSeen...)
	}
	if len(s.EntitiesSeen) > 0 {
		m["entities_seen"] = append([]string(nil), s.EntitiesSeen...)
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
	if len(m) == 0 {
		return nil
	}
	return m
}
