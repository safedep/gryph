package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/decision"
)

// SpoolRecorder records the actions that a hook client decided without the
// decision service. Every row gets a receipt with the decision unverified,
// in the chain of the agent session, so a review sees what ran while the
// service was out of reach.
type SpoolRecorder struct {
	service  *decision.Local
	receipts receipt.Generator
}

// SpoolRecorder returns the recorder of this runtime's store.
func (a *Runtime) SpoolRecorder() (*SpoolRecorder, error) {
	if a.Store == nil {
		return nil, errors.New("engine: the store is not open")
	}
	gen, err := newReceiptGenerator(a.Config, a.Paths, a.Store)
	if err != nil {
		return nil, err
	}
	return &SpoolRecorder{service: a.decisionService(), receipts: gen}, nil
}

// Record stores the action of req with the client's verdict and reason.
func (r *SpoolRecorder) Record(ctx context.Context, req *decision.HookRequest, verdict decision.Verdict, reason string) error {
	event, err := r.service.RecordDecided(ctx, req, verdict)
	if err != nil {
		return err
	}
	actionID := uuid.New()
	_, err = r.receipts.Record(ctx, &receipt.RecordInput{
		SessionID: event.SessionID,
		ActionID:  actionID,
		EventID:   event.ID,
		Agent:     event.AgentName,
		Action: &model.Action{
			ID:             actionID,
			Timestamp:      event.Timestamp,
			SessionID:      event.SessionID,
			EventID:        event.ID,
			Type:           model.ActionType(event.ActionType),
			Tool:           event.ToolName,
			Agent:          event.AgentName,
			AgentSessionID: event.AgentSessionID,
		},
		Decision: &model.EvaluationResult{
			Decision: receipt.DecisionUnverified,
			Severity: model.SeverityInfo,
			Message:  fmt.Sprintf("client verdict %s without the decision service: %s", verdict, reason),
		},
	})
	if err != nil {
		return fmt.Errorf("spool: record receipt: %w", err)
	}
	return nil
}
