package decision

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/utils/projectdetection"
)

// ResultRecorder records the execution outcome of an allowed action. The
// AARM Mediator implements it.
type ResultRecorder interface {
	RecordResult(ctx context.Context, actionID, sessionID uuid.UUID, sequence int64, result model.Result) error
}

// Local implements Service in the hook process.
type Local struct {
	store        storage.Store
	evaluator    *security.Evaluator
	redactor     *privacy.Redactor
	loggingLevel func(agent string) config.LoggingLevel
	recorder     func() ResultRecorder
	onSessionEnd func(*session.Session)
	hookSpec     HookSpecLookup
	classifier   Classifier
}

// Classifier returns the classes of the content an event holds. The
// aarm/classify heuristic implements it.
type Classifier interface {
	ClassifyEvent(event *events.Event) []privacy.Class
}

// HookSpecLookup returns the declared spec of an agent's hook type.
type HookSpecLookup func(agent string, hook events.HookType) (events.HookSpec, bool)

var _ Service = (*Local)(nil)

// LocalOption configures optional Local dependencies.
type LocalOption func(*Local)

// WithResultRecorder installs the source of the post-hook result recorder.
// The function may return nil when no recorder is available, for example
// when the policy layer is disabled or has not loaded.
func WithResultRecorder(fn func() ResultRecorder) LocalOption {
	return func(l *Local) {
		l.recorder = fn
	}
}

// WithSessionEndHook installs a callback that runs when a session ends,
// before the ended session is saved.
func WithSessionEndHook(fn func(*session.Session)) LocalOption {
	return func(l *Local) {
		l.onSessionEnd = fn
	}
}

// WithHookSpecs installs the lookup the service uses to set an event's
// phase from the adapter's declared hooks.
func WithHookSpecs(lookup HookSpecLookup) LocalOption {
	return func(l *Local) {
		l.hookSpec = lookup
	}
}

// WithClassifier installs the classifier that sets the classes of each
// content label.
func WithClassifier(c Classifier) LocalOption {
	return func(l *Local) {
		l.classifier = c
	}
}

// NewLocal creates the in-process decision service.
func NewLocal(store storage.Store, evaluator *security.Evaluator, redactor *privacy.Redactor,
	loggingLevel func(agent string) config.LoggingLevel, opts ...LocalOption) *Local {
	l := &Local{
		store:        store,
		evaluator:    evaluator,
		redactor:     redactor,
		loggingLevel: loggingLevel,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Handle implements Service.
func (l *Local) Handle(ctx context.Context, req *HookRequest) (*HookResponse, error) {
	if l.store == nil || l.evaluator == nil {
		return nil, fmt.Errorf("decision: service is not initialized")
	}

	event := req.event()

	var classes []privacy.Class
	if l.classifier != nil {
		classes = l.classifier.ClassifyEvent(event)
	}
	labelEvent(event, l.redactor, classes)

	sess, err := l.loadSession(ctx, event)
	if err != nil {
		return nil, err
	}

	l.classify(ctx, event)

	result := l.evaluator.Evaluate(ctx, event, sess)
	applyLevel(event, l.loggingLevel(event.AgentName))
	if !result.IsAllowed() {
		l.recordBlocked(ctx, sess, event, result)
		return &HookResponse{Decision: VerdictOf(security.DecisionBlock), Reason: result.BlockReason}, nil
	}

	if err := l.recordAllowed(ctx, sess, event); err != nil {
		return nil, err
	}

	l.recordResult(ctx, result, event)

	if result.FinalDecision == security.DecisionGuidance {
		return &HookResponse{Decision: VerdictOf(security.DecisionGuidance), Guidance: result.AggregatedGuidance()}, nil
	}
	return &HookResponse{Decision: VerdictOf(security.DecisionAllow)}, nil
}

// classify sets the event's phase from the adapter's hook spec, links a post
// event to the pre event of the same tool call, and sets the kind. The
// service is the only source of these fields, so it clears the values the
// request carries. A failed link lookup keeps the event unlinked, because
// the link is extra data and the event must still be recorded.
func (l *Local) classify(ctx context.Context, event *events.Event) {
	event.Phase = events.PhaseUnknown
	event.LinkedEventID = uuid.Nil
	if l.hookSpec != nil && event.HookType != "" {
		if spec, ok := l.hookSpec(event.AgentName, event.HookType); ok {
			event.Phase = spec.Phase
		} else {
			log.Warnf("decision: %s hook %q is not declared by the adapter", event.AgentName, event.HookType)
		}
	}

	if event.Phase == events.PhasePost && event.ToolCallID != "" {
		pre, err := l.store.FindPreEventByToolCall(ctx, event.SessionID, event.ToolCallID)
		switch {
		case err != nil:
			log.Warnf("decision: link tool call %s: %v", event.ToolCallID, err)
		case pre != nil:
			event.LinkedEventID = pre.ID
		}
	}

	event.Kind = events.KindOf(event, event.LinkedEventID != uuid.Nil)
}

func (l *Local) loadSession(ctx context.Context, event *events.Event) (*session.Session, error) {
	sess, err := l.store.GetSession(ctx, event.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if sess == nil {
		sess = session.NewSessionWithID(event.SessionID, event.AgentName)
		sess.AgentSessionID = event.AgentSessionID
		sess.WorkingDirectory = event.WorkingDirectory
		sess.TranscriptPath = event.TranscriptPath

		if event.WorkingDirectory != "" {
			if info, err := projectdetection.DetectProject(event.WorkingDirectory); err == nil && info != nil && info.Name != "" {
				sess.ProjectName = info.Name
			} else {
				sess.ProjectName = filepath.Base(event.WorkingDirectory)
			}
		}

		if err := l.store.SaveSession(ctx, sess); err != nil {
			existing, getErr := l.store.GetSession(ctx, event.SessionID)
			if getErr != nil || existing == nil {
				return nil, fmt.Errorf("failed to save session: %w", err)
			}

			sess = existing
		}
	}

	if sess.TranscriptPath == "" && event.TranscriptPath != "" {
		sess.TranscriptPath = event.TranscriptPath
	}

	return sess, nil
}

// recordBlocked saves the blocked event with the stored block reason. The
// agent gets the full reason. The stored reason comes from the action that
// the receipt records, so it does not hold content that applyLevel removed.
// The redactor runs on it again, because a check can put any text in it.
func (l *Local) recordBlocked(ctx context.Context, sess *session.Session, event *events.Event, result *security.Result) {
	event.ResultStatus = events.ResultBlocked
	event.ErrorMessage = result.StoredBlockReason
	if l.redactor != nil {
		event.ErrorMessage = l.redactor.Redact(event.ErrorMessage)
	}
	event.Sequence = sess.TotalActions + 1

	if err := l.store.SaveEvent(ctx, event); err != nil {
		log.Errorf("failed to save blocked event: %v", err)
	}

	sess.TotalActions++
	sess.BlockedActions++
	if event.IsSensitive {
		sess.SensitiveActions++
	}

	if err := l.store.UpdateSession(ctx, sess); err != nil {
		log.Errorf("failed to update session for blocked event: %v", err)
	}
}

func (l *Local) recordAllowed(ctx context.Context, sess *session.Session, event *events.Event) error {
	event.Sequence = sess.TotalActions + 1

	if err := l.store.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	sess.TotalActions++
	switch event.ActionType {
	case events.ActionFileRead:
		sess.FilesRead++
	case events.ActionFileWrite:
		sess.FilesWritten++
	case events.ActionCommandExec:
		sess.CommandsExecuted++
	}

	if event.ResultStatus == events.ResultError {
		sess.Errors++
	}

	if event.IsSensitive {
		sess.SensitiveActions++
	}

	if err := l.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	if event.ActionType == events.ActionSessionEnd {
		sess.End()
		if l.onSessionEnd != nil {
			l.onSessionEnd(sess)
		}
		if err := l.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("failed to end session: %w", err)
		}
	}

	return nil
}

// recordResult sends the post-hook execution outcome to the AARM layer on the
// allow path. The service only sees the hook's own exit, so the recorded
// status is success unless the event reports an error.
func (l *Local) recordResult(ctx context.Context, result *security.Result, event *events.Event) {
	if l.recorder == nil {
		return
	}
	recorder := l.recorder()
	if recorder == nil {
		return
	}
	actionID, sessionID, sequence := result.AarmRef()
	if actionID == uuid.Nil && sequence == 0 {
		return
	}
	outcome := model.Result{Status: model.ResultSuccess}
	if event.ResultStatus == events.ResultError {
		outcome.Status = model.ResultError
		outcome.Error = event.ErrorMessage
	}
	if err := recorder.RecordResult(ctx, actionID, sessionID, sequence, outcome); err != nil {
		log.Warnf("aarm: post-hook record result: %v", err)
	}
}
