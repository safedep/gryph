package decision

import (
	"context"
	"fmt"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
)

// RecordDecided records an action that the hook side decided without the
// service and left in the spool. The service labels, classifies and
// stores the event as it does on Handle, but runs no evaluation: the
// verdict is the client's, and the accumulator never sees the action. The
// caller writes the receipt that marks the row as unverified.
func (l *Local) RecordDecided(ctx context.Context, req *HookRequest, verdict Verdict) (*events.Event, error) {
	if l.store == nil {
		return nil, fmt.Errorf("decision: service is not initialized")
	}

	event := req.event()
	event.ClaimOrigin()

	var classes []privacy.Class
	if l.classifier != nil {
		classes = l.classifier.ClassifyEvent(event)
	}
	labelEvent(event, l.redactor, classes)

	sess, err := l.loadSession(ctx, event, req.Project)
	if err != nil {
		return nil, err
	}
	l.classify(ctx, event)
	applyLevel(event, l.loggingLevel(event.AgentName))

	if d, ok := verdict.Decision(); ok && d == security.DecisionBlock {
		event.ResultStatus = events.ResultBlocked
		if err := l.recordEvent(ctx, sess, event); err != nil {
			return nil, fmt.Errorf("failed to save event: %w", err)
		}
		return event, nil
	}
	if err := l.recordAllowed(ctx, sess, event, req.Cost); err != nil {
		return nil, err
	}
	return event, nil
}
