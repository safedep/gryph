package decision

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fullRequest() *HookRequest {
	ts := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	event := events.NewEvent(uuid.New(), "claude-code", events.ActionCommandExec)
	event.Timestamp = ts
	event.AgentSessionID = "agent-session"
	event.Sequence = 3
	event.DurationMs = 12
	event.AgentVersion = "1.0.0"
	event.WorkingDirectory = "/work"
	event.ToolName = "Bash"
	event.ResultStatus = events.ResultError
	event.ErrorMessage = "boom"
	event.Payload = json.RawMessage(`{"command":"ls"}`)
	event.DiffContent = privacy.NewText("diff")
	event.RawEvent = json.RawMessage(`{"raw":true}`)
	event.IsSensitive = true
	event.SubagentID = "sub-1"
	event.SubagentType = "Explore"
	event.TranscriptPath = "/tmp/transcript.jsonl"
	event.HookType = "PreToolUse"
	event.FullContent = "full content"

	return NewHookRequest(event)
}

func TestHookRequest_JSONRoundTrip(t *testing.T) {
	req := fullRequest()

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got HookRequest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, req.event(), got.event())
	assert.Equal(t, "claude-code", got.Event.AgentName)
	assert.Equal(t, req.HookType, got.HookType)
}

func TestHookRequest_CarriesInMemoryEventFields(t *testing.T) {
	req := fullRequest()
	event := req.event()

	assert.Equal(t, "/tmp/transcript.jsonl", event.TranscriptPath)
	assert.Equal(t, events.HookType("PreToolUse"), event.HookType)
	assert.Equal(t, "full content", event.FullContent)

	assert.Empty(t, req.Event.TranscriptPath, "each value has one source")
	assert.Empty(t, req.Event.HookType)
	assert.Empty(t, req.Event.FullContent)
}

func TestHookResponse_JSONRoundTrip(t *testing.T) {
	resp := &HookResponse{Decision: VerdictOf(security.DecisionGuidance), Reason: "r", Guidance: "g"}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.JSONEq(t, `{"decision":"guidance","reason":"r","guidance":"g"}`, string(data))

	var got HookResponse
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, resp, &got)
}

func TestHookResponse_DecodeFailsClosed(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantVerdict Verdict
	}{
		{name: "missing decision", body: `{}`, wantVerdict: ""},
		{name: "empty decision", body: `{"decision":""}`, wantVerdict: ""},
		{name: "decision from a newer service", body: `{"decision":"defer"}`, wantVerdict: "defer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got HookResponse
			require.NoError(t, json.Unmarshal([]byte(tc.body), &got))
			assert.Equal(t, tc.wantVerdict, got.Decision)

			_, ok := got.Decision.Decision()
			assert.False(t, ok)
		})
	}
}

func TestVerdict_RoundTrip(t *testing.T) {
	for _, d := range []security.Decision{security.DecisionAllow, security.DecisionBlock, security.DecisionGuidance} {
		got, ok := VerdictOf(d).Decision()
		assert.True(t, ok, d.String())
		assert.Equal(t, d, got)
	}

	_, ok := VerdictOf(security.Decision(99)).Decision()
	assert.False(t, ok)
}

func TestLocal_Handle_AgentComesFromEvent(t *testing.T) {
	var levelAgent string
	level := func(agent string) config.LoggingLevel {
		levelAgent = agent
		return config.LoggingFull
	}

	ctx := context.Background()
	store := storagetest.NewStore(t)
	sessionID := uuid.New()
	req := writeRequest(sessionID)
	req.Event.AgentName = "cursor"

	svc := NewLocal(store, security.New(&security.Config{FailOpen: true}), nil, level)
	_, err := svc.Handle(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, "cursor", levelAgent)
	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "cursor", sess.AgentName)
}

// TestBoundaryTypes_DataOnly keeps the request and response serializable
// across a process boundary.
func TestBoundaryTypes_DataOnly(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(HookRequest{}), reflect.TypeOf(HookResponse{})} {
		assertDataOnly(t, typ, typ.Name())
	}
}

func assertDataOnly(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	switch typ.Kind() {
	case reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
		assert.Failf(t, "boundary type holds a non-data field", "%s is %s", path, typ.Kind())
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			assertDataOnly(t, f.Type, path+"."+f.Name)
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertDataOnly(t, typ.Elem(), path)
	case reflect.Map:
		assertDataOnly(t, typ.Key(), path)
		assertDataOnly(t, typ.Elem(), path)
	}
}

type blockCheck struct{}

func (blockCheck) Name() string  { return "test-block" }
func (blockCheck) Enabled() bool { return true }
func (blockCheck) Check(context.Context, *events.Event, *session.Session) (*security.CheckResult, error) {
	return &security.CheckResult{CheckName: "test-block", Decision: security.DecisionBlock, Reason: "blocked by test"}, nil
}

type guidanceCheck struct{}

func (guidanceCheck) Name() string  { return "test-guidance" }
func (guidanceCheck) Enabled() bool { return true }
func (guidanceCheck) Check(context.Context, *events.Event, *session.Session) (*security.CheckResult, error) {
	return &security.CheckResult{CheckName: "test-guidance", Decision: security.DecisionGuidance, Guidance: "be careful"}, nil
}

type aarmRefCheck struct{ actionID, sessionID uuid.UUID }

func (aarmRefCheck) Name() string  { return "test-aarm" }
func (aarmRefCheck) Enabled() bool { return true }
func (c aarmRefCheck) Check(context.Context, *events.Event, *session.Session) (*security.CheckResult, error) {
	return &security.CheckResult{CheckName: "test-aarm", Decision: security.DecisionAllow,
		AarmActionID: c.actionID, AarmSessionID: c.sessionID, AarmSequence: 7}, nil
}

type fakeRecorder struct {
	calls  int
	result model.Result
	seq    int64
}

func (r *fakeRecorder) RecordResult(_ context.Context, _, _ uuid.UUID, sequence int64, result model.Result) error {
	r.calls++
	r.seq = sequence
	r.result = result
	return nil
}

func fullLevel(string) config.LoggingLevel { return config.LoggingFull }

func writeRequest(sessionID uuid.UUID) *HookRequest {
	event := events.NewEvent(sessionID, "claude-code", events.ActionFileWrite)
	event.WorkingDirectory = "/work/project"
	event.Payload = json.RawMessage(`{"path":"/work/project/a.txt","content_preview":"password=hunter2"}`)
	return NewHookRequest(event)
}

func TestLocal_Handle(t *testing.T) {
	cases := []struct {
		name          string
		checks        []security.Check
		wantDecision  Verdict
		wantReason    string
		wantGuidance  string
		wantStatus    events.ResultStatus
		wantBlocked   int
		wantWritten   int
		wantPersisted int
	}{
		{name: "allow", wantDecision: VerdictOf(security.DecisionAllow), wantStatus: events.ResultSuccess, wantWritten: 1, wantPersisted: 1},
		{name: "block", checks: []security.Check{blockCheck{}}, wantDecision: VerdictOf(security.DecisionBlock),
			wantReason: "blocked by test", wantStatus: events.ResultBlocked, wantBlocked: 1, wantWritten: 1, wantPersisted: 1},
		{name: "guidance", checks: []security.Check{guidanceCheck{}}, wantDecision: VerdictOf(security.DecisionGuidance),
			wantGuidance: "be careful", wantStatus: events.ResultSuccess, wantWritten: 1, wantPersisted: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := storagetest.NewStore(t)
			evaluator := security.New(&security.Config{FailOpen: true})
			for _, c := range tc.checks {
				evaluator.RegisterCheck(c)
			}
			redactor, err := privacy.NewRedactor(nil, privacy.DefaultRedactPatterns())
			require.NoError(t, err)

			svc := NewLocal(store, evaluator, redactor, fullLevel)
			sessionID := uuid.New()
			resp, err := svc.Handle(ctx, writeRequest(sessionID))
			require.NoError(t, err)

			assert.Equal(t, tc.wantDecision, resp.Decision)
			assert.Equal(t, tc.wantReason, resp.Reason)
			assert.Equal(t, tc.wantGuidance, resp.Guidance)

			sess, err := store.GetSession(ctx, sessionID)
			require.NoError(t, err)
			require.NotNil(t, sess)
			assert.Equal(t, 1, sess.TotalActions)
			assert.Equal(t, tc.wantBlocked, sess.BlockedActions)
			assert.Equal(t, tc.wantWritten, sess.FilesWritten)
			assert.Equal(t, "project", sess.ProjectName)

			stored, err := store.QueryEvents(ctx, events.NewEventFilter().WithSession(sessionID))
			require.NoError(t, err)
			require.Len(t, stored, tc.wantPersisted)
			assert.Equal(t, tc.wantStatus, stored[0].ResultStatus)
			assert.NotContains(t, string(stored[0].Payload), "hunter2")
		})
	}
}

func TestLocal_Handle_ResultRecorder(t *testing.T) {
	cases := []struct {
		name       string
		checks     []security.Check
		nilSource  bool
		eventError bool
		wantCalls  int
		wantStatus model.ResultStatus
		wantError  string
	}{
		{name: "allow records success", wantCalls: 1, wantStatus: model.ResultSuccess},
		{name: "allow records error outcome", eventError: true, wantCalls: 1,
			wantStatus: model.ResultError, wantError: "boom"},
		{name: "block records nothing", checks: []security.Check{blockCheck{}}, wantCalls: 0},
		{name: "nil recorder is skipped", nilSource: true, wantCalls: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evaluator := security.New(&security.Config{FailOpen: true})
			evaluator.RegisterCheck(aarmRefCheck{actionID: uuid.New(), sessionID: uuid.New()})
			for _, c := range tc.checks {
				evaluator.RegisterCheck(c)
			}
			recorder := &fakeRecorder{}
			source := func() ResultRecorder { return recorder }
			if tc.nilSource {
				source = func() ResultRecorder { return nil }
			}

			req := writeRequest(uuid.New())
			if tc.eventError {
				req.Event.ResultStatus = events.ResultError
				req.Event.ErrorMessage = "boom"
			}

			svc := NewLocal(storagetest.NewStore(t), evaluator, nil, fullLevel, WithResultRecorder(source))
			_, err := svc.Handle(context.Background(), req)
			require.NoError(t, err)

			assert.Equal(t, tc.wantCalls, recorder.calls)
			if tc.wantCalls > 0 {
				assert.Equal(t, int64(7), recorder.seq)
				assert.Equal(t, tc.wantStatus, recorder.result.Status)
				assert.Equal(t, tc.wantError, recorder.result.Error)
			}
		})
	}
}

type captureCheck struct {
	seen     *events.Event
	sessions []*session.Session
}

func (*captureCheck) Name() string  { return "test-capture" }
func (*captureCheck) Enabled() bool { return true }
func (c *captureCheck) Check(_ context.Context, event *events.Event, sess *session.Session) (*security.CheckResult, error) {
	copied := *event
	c.seen = &copied
	c.sessions = append(c.sessions, sess)
	return &security.CheckResult{CheckName: "test-capture", Decision: security.DecisionAllow}, nil
}

func TestLocal_Handle_PassesStoredSessionToEvaluator(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	capture := &captureCheck{}
	evaluator := security.New(&security.Config{FailOpen: true})
	evaluator.RegisterCheck(capture)
	svc := NewLocal(store, evaluator, nil, fullLevel)
	sessionID := uuid.New()

	for range 2 {
		_, err := svc.Handle(ctx, writeRequest(sessionID))
		require.NoError(t, err)
	}

	stored, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, capture.sessions, 2)
	for _, got := range capture.sessions {
		require.NotNil(t, got, "a nil session fails open on project-scoped rules")
		assert.Equal(t, stored.ID, got.ID)
		assert.Equal(t, "project", got.ProjectName)
		assert.True(t, stored.StartedAt.Equal(got.StartedAt))
	}
}

func TestLocal_Handle_InMemoryFieldsReachEvaluator(t *testing.T) {
	capture := &captureCheck{}
	evaluator := security.New(&security.Config{FailOpen: true})
	evaluator.RegisterCheck(capture)

	event := events.NewEvent(uuid.New(), "claude-code", events.ActionFileWrite)
	event.HookType = "PreToolUse"
	event.FullContent = "full body"
	event.TranscriptPath = "/tmp/t.jsonl"

	svc := NewLocal(storagetest.NewStore(t), evaluator, nil, fullLevel)
	_, err := svc.Handle(context.Background(), NewHookRequest(event))
	require.NoError(t, err)

	require.NotNil(t, capture.seen)
	assert.Equal(t, events.HookType("PreToolUse"), capture.seen.HookType)
	assert.Equal(t, "full body", capture.seen.FullContent)
	assert.Equal(t, "/tmp/t.jsonl", capture.seen.TranscriptPath)
}

func TestLocal_Handle_SequenceAndTranscriptBackfill(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	svc := NewLocal(store, security.New(&security.Config{FailOpen: true}), nil, fullLevel)
	sessionID := uuid.New()

	_, err := svc.Handle(ctx, writeRequest(sessionID))
	require.NoError(t, err)

	second := events.NewEvent(sessionID, "claude-code", events.ActionFileRead)
	second.TranscriptPath = "/tmp/late.jsonl"
	_, err = svc.Handle(ctx, NewHookRequest(second))
	require.NoError(t, err)

	stored, err := store.QueryEvents(ctx, events.NewEventFilter().WithSession(sessionID))
	require.NoError(t, err)
	require.Len(t, stored, 2)
	sequences := []int{stored[0].Sequence, stored[1].Sequence}
	assert.ElementsMatch(t, []int{1, 2}, sequences)

	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 2, sess.TotalActions)
	assert.Equal(t, 1, sess.FilesRead)
	assert.Equal(t, "/tmp/late.jsonl", sess.TranscriptPath)
}

// faultStore wraps a real store and fails selected writes.
type faultStore struct {
	storage.Store
	saveSessionRaces bool
	failSaveEvent    bool
	failLink         bool
}

func (f *faultStore) FindPreEventByToolCall(ctx context.Context, sessionID uuid.UUID, toolCallID string) (*events.Event, error) {
	if f.failLink {
		return nil, errors.New("database is locked")
	}
	return f.Store.FindPreEventByToolCall(ctx, sessionID, toolCallID)
}

func (f *faultStore) SaveSession(ctx context.Context, sess *session.Session) error {
	if f.saveSessionRaces {
		if err := f.Store.SaveSession(ctx, sess); err != nil {
			return err
		}
		return errors.New("already exists")
	}
	return f.Store.SaveSession(ctx, sess)
}

func (f *faultStore) RecordEvent(ctx context.Context, event *events.Event, counts session.Counts) error {
	if f.failSaveEvent {
		return errors.New("disk full")
	}
	return f.Store.RecordEvent(ctx, event, counts)
}

func TestLocal_Handle_StoreFaults(t *testing.T) {
	cases := []struct {
		name         string
		store        func(t *testing.T) *faultStore
		checks       []security.Check
		wantErr      string
		wantDecision Verdict
	}{
		{name: "session save race uses the existing session",
			store: func(t *testing.T) *faultStore {
				return &faultStore{Store: storagetest.NewStore(t), saveSessionRaces: true}
			},
			wantDecision: VerdictOf(security.DecisionAllow)},
		{name: "event save failure on allow is returned",
			store: func(t *testing.T) *faultStore {
				return &faultStore{Store: storagetest.NewStore(t), failSaveEvent: true}
			},
			wantErr: "failed to save event"},
		{name: "event save failure on block is logged",
			store: func(t *testing.T) *faultStore {
				return &faultStore{Store: storagetest.NewStore(t), failSaveEvent: true}
			},
			checks:       []security.Check{blockCheck{}},
			wantDecision: VerdictOf(security.DecisionBlock)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evaluator := security.New(&security.Config{FailOpen: true})
			for _, c := range tc.checks {
				evaluator.RegisterCheck(c)
			}
			svc := NewLocal(tc.store(t), evaluator, nil, fullLevel)
			resp, err := svc.Handle(context.Background(), writeRequest(uuid.New()))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantDecision, resp.Decision)
		})
	}
}

func TestLocal_Handle_SessionEndRunsHook(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	evaluator := security.New(&security.Config{FailOpen: true})
	var ended *session.Session

	svc := NewLocal(store, evaluator, nil, fullLevel,
		WithSessionEndHook(func(s *session.Session) {
			assert.False(t, s.EndedAt.IsZero(), "the hook sees an ended session")
			s.ProjectName = "set-by-hook"
			ended = s
		}))

	sessionID := uuid.New()
	event := events.NewEvent(sessionID, "claude-code", events.ActionSessionEnd)
	_, err := svc.Handle(ctx, NewHookRequest(event))
	require.NoError(t, err)

	require.NotNil(t, ended)
	assert.Equal(t, sessionID, ended.ID)
	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.False(t, sess.EndedAt.IsZero())
	assert.Equal(t, "set-by-hook", sess.ProjectName, "the hook runs before the session is saved")
}

func TestLocal_Handle_NotInitialized(t *testing.T) {
	svc := NewLocal(nil, nil, nil, fullLevel)
	_, err := svc.Handle(context.Background(), writeRequest(uuid.New()))
	require.Error(t, err)
}

func testHookSpecs(_ string, hook events.HookType) (events.HookSpec, bool) {
	switch hook {
	case "PreToolUse":
		return events.HookSpec{Type: hook, Phase: events.PhasePre, Blocking: true}, true
	case "PostToolUse":
		return events.HookSpec{Type: hook, Phase: events.PhasePost}, true
	case "SessionStart":
		return events.HookSpec{Type: hook, Phase: events.PhaseUnknown}, true
	}
	return events.HookSpec{}, false
}

func toolEvent(sessionID uuid.UUID, hook events.HookType, toolCallID string) *events.Event {
	event := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
	event.HookType = hook
	event.ToolCallID = toolCallID
	event.Payload = json.RawMessage(`{"command":"ls"}`)
	return event
}

func TestLocal_Handle_ClassifiesEvents(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	svc := NewLocal(store, security.New(&security.Config{FailOpen: true}), nil, fullLevel, WithHookSpecs(testHookSpecs))
	sessionID := uuid.New()

	pre := toolEvent(sessionID, "PreToolUse", "tu-1")
	post := toolEvent(sessionID, "PostToolUse", "tu-1")
	postOnly := toolEvent(sessionID, "PostToolUse", "tu-2")
	noID := toolEvent(sessionID, "PostToolUse", "")
	undeclared := toolEvent(sessionID, "Mystery", "")
	otherSession := toolEvent(uuid.New(), "PostToolUse", "tu-1")
	forged := toolEvent(sessionID, "PreToolUse", "tu-3")
	forged.LinkedEventID = pre.ID

	for _, e := range []*events.Event{pre, post, postOnly, noID, undeclared, otherSession, forged} {
		_, err := svc.Handle(ctx, NewHookRequest(e))
		require.NoError(t, err)
	}

	cases := []struct {
		name       string
		id         uuid.UUID
		wantPhase  events.Phase
		wantKind   events.Kind
		wantLinked uuid.UUID
	}{
		{name: "pre hook is an action", id: pre.ID, wantPhase: events.PhasePre, wantKind: events.KindAction},
		{name: "post hook with a recorded pre hook is an observation", id: post.ID,
			wantPhase: events.PhasePost, wantKind: events.KindObservation, wantLinked: pre.ID},
		{name: "post hook without a recorded pre hook is an action", id: postOnly.ID,
			wantPhase: events.PhasePost, wantKind: events.KindAction},
		{name: "post hook without a tool call id is an action", id: noID.ID,
			wantPhase: events.PhasePost, wantKind: events.KindAction},
		{name: "undeclared hook has phase unknown", id: undeclared.ID,
			wantPhase: events.PhaseUnknown, wantKind: events.KindAction},
		{name: "post hook does not link to another session", id: otherSession.ID,
			wantPhase: events.PhasePost, wantKind: events.KindAction},
		{name: "service ignores a link in the request", id: forged.ID,
			wantPhase: events.PhasePre, wantKind: events.KindAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored, err := store.GetEvent(ctx, tc.id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tc.wantPhase, stored.Phase)
			assert.Equal(t, tc.wantKind, stored.Kind)
			assert.Equal(t, tc.wantLinked, stored.LinkedEventID)
		})
	}
}

func TestLocal_Handle_PhaseReachesEvaluator(t *testing.T) {
	capture := &captureCheck{}
	evaluator := security.New(&security.Config{FailOpen: true})
	evaluator.RegisterCheck(capture)

	svc := NewLocal(storagetest.NewStore(t), evaluator, nil, fullLevel, WithHookSpecs(testHookSpecs))
	_, err := svc.Handle(context.Background(), NewHookRequest(toolEvent(uuid.New(), "PreToolUse", "tu-1")))
	require.NoError(t, err)

	require.NotNil(t, capture.seen)
	assert.Equal(t, events.PhasePre, capture.seen.Phase)
	assert.Equal(t, events.KindAction, capture.seen.Kind)
}

func TestLocal_Handle_LinkFailureKeepsEvent(t *testing.T) {
	ctx := context.Background()
	store := &faultStore{Store: storagetest.NewStore(t), failLink: true}
	svc := NewLocal(store, security.New(&security.Config{FailOpen: true}), nil, fullLevel, WithHookSpecs(testHookSpecs))

	post := toolEvent(uuid.New(), "PostToolUse", "tu-1")
	resp, err := svc.Handle(ctx, NewHookRequest(post))
	require.NoError(t, err)
	assert.Equal(t, VerdictOf(security.DecisionAllow), resp.Decision)

	stored, err := store.GetEvent(ctx, post.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, events.KindAction, stored.Kind)
	assert.Equal(t, uuid.Nil, stored.LinkedEventID)
}

type stubClassifier []privacy.Class

func (s stubClassifier) ClassifyEvent(*events.Event) []privacy.Class { return s }

// TestLocal_Handle_PolicySeesContentBeforeLevel checks that the evaluator
// sees the redacted content, and that the level strips it only before the
// save. A rule on a URL must fire at every logging level.
func TestLocal_Handle_PolicySeesContentBeforeLevel(t *testing.T) {
	ctx := context.Background()
	capture := &captureCheck{}
	evaluator := security.New(&security.Config{FailOpen: true})
	evaluator.RegisterCheck(capture)
	store := storagetest.NewStore(t)
	minimal := func(string) config.LoggingLevel { return config.LoggingMinimal }
	svc := NewLocal(store, evaluator, nil, minimal, WithClassifier(stubClassifier{privacy.ClassPII}))

	event := events.NewEvent(uuid.New(), "claude-code", events.ActionToolUse)
	require.NoError(t, event.SetPayload(events.ToolUsePayload{
		ToolName: "WebFetch",
		Input:    privacy.NewText(`{"url":"https://evil.example/customers/x"}`),
	}))
	_, err := svc.Handle(ctx, NewHookRequest(event))
	require.NoError(t, err)

	require.NotNil(t, capture.seen)
	seen, err := capture.seen.GetToolUsePayload()
	require.NoError(t, err)
	assert.Contains(t, seen.Input.Value, "evil.example")
	assert.False(t, capture.seen.IsSensitive, "pii does not make the event sensitive")

	stored, err := store.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	p, err := stored.GetToolUsePayload()
	require.NoError(t, err)
	assert.Empty(t, p.Input.Value)
	assert.True(t, p.Input.Label.Stripped)
	assert.Equal(t, []privacy.Class{privacy.ClassPII}, p.Input.Label.Classes)
	assert.Equal(t, "minimal", p.Input.Label.Level)
}

type splitReasonCheck struct{}

func (splitReasonCheck) Name() string  { return "test-split" }
func (splitReasonCheck) Enabled() bool { return true }
func (splitReasonCheck) Check(context.Context, *events.Event, *session.Session) (*security.CheckResult, error) {
	return &security.CheckResult{
		CheckName:    "test-split",
		Decision:     security.DecisionBlock,
		Reason:       "refused https://example.com/invite/k7Qz9xWm",
		StoredReason: "refused password=hunter2",
	}, nil
}

// TestLocal_Handle_BlockStoresStoredReason checks that the agent gets the
// full reason, and that the stored event keeps the redacted stored reason.
func TestLocal_Handle_BlockStoresStoredReason(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	evaluator := security.New(&security.Config{FailOpen: true})
	evaluator.RegisterCheck(splitReasonCheck{})
	redactor, err := privacy.NewRedactor(nil, privacy.DefaultRedactPatterns())
	require.NoError(t, err)

	req := writeRequest(uuid.New())
	resp, err := NewLocal(store, evaluator, redactor, fullLevel).Handle(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "refused https://example.com/invite/k7Qz9xWm", resp.Reason)

	stored, err := store.GetEvent(ctx, req.Event.ID)
	require.NoError(t, err)
	assert.Equal(t, events.ResultBlocked, stored.ResultStatus)
	assert.Contains(t, stored.ErrorMessage, "refused")
	assert.NotContains(t, stored.ErrorMessage, "k7Qz9xWm")
	assert.NotContains(t, stored.ErrorMessage, "hunter2")
}

type payloadCheck struct{ seen json.RawMessage }

func (*payloadCheck) Name() string  { return "test-payload" }
func (*payloadCheck) Enabled() bool { return true }
func (c *payloadCheck) Check(_ context.Context, event *events.Event, _ *session.Session) (*security.CheckResult, error) {
	c.seen = append(json.RawMessage(nil), event.Payload...)
	if _, err := event.GetCommandExecPayload(); err != nil {
		return nil, err
	}
	return &security.CheckResult{CheckName: "test-payload", Decision: security.DecisionAllow}, nil
}

func TestLocal_Handle_PayloadThatDoesNotDecodeFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := storagetest.NewStore(t)
	check := &payloadCheck{}
	evaluator := security.New(&security.Config{FailOpen: false})
	evaluator.RegisterCheck(check)
	svc := NewLocal(store, evaluator, nil, fullLevel)

	raw := json.RawMessage(`{"command":"rm -rf /","exit_code":"x"}`)
	event := events.NewEvent(uuid.New(), "claude-code", events.ActionCommandExec)
	event.Payload = raw
	resp, err := svc.Handle(ctx, NewHookRequest(event))
	require.NoError(t, err)

	assert.JSONEq(t, string(raw), string(check.seen))
	assert.Equal(t, VerdictOf(security.DecisionBlock), resp.Decision)

	stored, err := store.QueryEvents(ctx, events.NewEventFilter().WithSession(event.SessionID))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Empty(t, stored[0].Payload)
}
