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
	privacy      *events.PrivacyChecker
	loggingLevel func(agent string) config.LoggingLevel
	recorder     func() ResultRecorder
	onSessionEnd func(*session.Session)
}

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

// NewLocal creates the in-process decision service.
func NewLocal(store storage.Store, evaluator *security.Evaluator, privacy *events.PrivacyChecker,
	loggingLevel func(agent string) config.LoggingLevel, opts ...LocalOption) *Local {
	l := &Local{
		store:        store,
		evaluator:    evaluator,
		privacy:      privacy,
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

	// Order matters: redact configured patterns before the level filter strips
	// fields, so we never persist or log unredacted user content.
	redactEvent(event, l.privacy)
	applyLoggingLevel(event, l.loggingLevel(req.Agent))

	sess, err := l.loadSession(ctx, req.Agent, event)
	if err != nil {
		return nil, err
	}

	result := l.evaluator.Evaluate(session.WithSession(ctx, sess), event)
	if !result.IsAllowed() {
		l.recordBlocked(ctx, sess, event, result)
		return &HookResponse{Decision: security.DecisionBlock, Reason: result.BlockReason}, nil
	}

	if err := l.recordAllowed(ctx, sess, event); err != nil {
		return nil, err
	}

	l.recordResult(ctx, result, event)

	if result.FinalDecision == security.DecisionGuidance {
		return &HookResponse{Decision: security.DecisionGuidance, Guidance: result.AggregatedGuidance()}, nil
	}
	return &HookResponse{Decision: security.DecisionAllow}, nil
}

func (l *Local) loadSession(ctx context.Context, agentName string, event *events.Event) (*session.Session, error) {
	sess, err := l.store.GetSession(ctx, event.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if sess == nil {
		sess = session.NewSessionWithID(event.SessionID, agentName)
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

func (l *Local) recordBlocked(ctx context.Context, sess *session.Session, event *events.Event, result *security.Result) {
	event.ResultStatus = events.ResultBlocked
	event.ErrorMessage = result.BlockReason
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
