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
	event.DiffContent = "diff"
	event.RawEvent = json.RawMessage(`{"raw":true}`)
	event.ConversationContext = "context"
	event.IsSensitive = true
	event.SubagentID = "sub-1"
	event.SubagentType = "Explore"
	event.TranscriptPath = "/tmp/transcript.jsonl"
	event.HookType = "PreToolUse"
	event.FullContent = "full content"

	return NewHookRequest("claude-code", event)
}

func TestHookRequest_JSONRoundTrip(t *testing.T) {
	req := fullRequest()

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got HookRequest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, req.event(), got.event())
	assert.Equal(t, req.Agent, got.Agent)
	assert.Equal(t, req.HookType, got.HookType)
}

func TestHookRequest_CarriesInMemoryEventFields(t *testing.T) {
	req := fullRequest()
	event := req.event()

	assert.Equal(t, "/tmp/transcript.jsonl", event.TranscriptPath)
	assert.Equal(t, "PreToolUse", event.HookType)
	assert.Equal(t, "full content", event.FullContent)

	assert.Empty(t, req.Event.TranscriptPath, "each value has one source")
	assert.Empty(t, req.Event.HookType)
	assert.Empty(t, req.Event.FullContent)
}

func TestHookResponse_JSONRoundTrip(t *testing.T) {
	resp := &HookResponse{Decision: security.DecisionGuidance, Reason: "r", Guidance: "g"}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.JSONEq(t, `{"decision":"guidance","reason":"r","guidance":"g"}`, string(data))

	var got HookResponse
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, resp, &got)
}

func TestHookResponse_UnknownDecisionFailsToEncode(t *testing.T) {
	_, err := json.Marshal(&HookResponse{Decision: security.Decision(99)})
	require.Error(t, err)

	var got HookResponse
	require.Error(t, json.Unmarshal([]byte(`{"decision":"defer"}`), &got))
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
func (blockCheck) Check(context.Context, *events.Event) (*security.CheckResult, error) {
	return &security.CheckResult{CheckName: "test-block", Decision: security.DecisionBlock, Reason: "blocked by test"}, nil
}

type guidanceCheck struct{}

func (guidanceCheck) Name() string  { return "test-guidance" }
func (guidanceCheck) Enabled() bool { return true }
func (guidanceCheck) Check(context.Context, *events.Event) (*security.CheckResult, error) {
	return &security.CheckResult{CheckName: "test-guidance", Decision: security.DecisionGuidance, Guidance: "be careful"}, nil
}

type aarmRefCheck struct{ actionID, sessionID uuid.UUID }

func (aarmRefCheck) Name() string  { return "test-aarm" }
func (aarmRefCheck) Enabled() bool { return true }
func (c aarmRefCheck) Check(context.Context, *events.Event) (*security.CheckResult, error) {
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
	return NewHookRequest("claude-code", event)
}

func TestLocal_Handle(t *testing.T) {
	cases := []struct {
		name          string
		checks        []security.Check
		wantDecision  security.Decision
		wantReason    string
		wantGuidance  string
		wantStatus    events.ResultStatus
		wantBlocked   int
		wantWritten   int
		wantPersisted int
	}{
		{name: "allow", wantDecision: security.DecisionAllow, wantStatus: events.ResultSuccess, wantWritten: 1, wantPersisted: 1},
		{name: "block", checks: []security.Check{blockCheck{}}, wantDecision: security.DecisionBlock,
			wantReason: "blocked by test", wantStatus: events.ResultBlocked, wantBlocked: 1, wantPersisted: 1},
		{name: "guidance", checks: []security.Check{guidanceCheck{}}, wantDecision: security.DecisionGuidance,
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
			privacy, err := events.NewPrivacyChecker(nil, events.DefaultRedactPatterns())
			require.NoError(t, err)

			svc := NewLocal(store, evaluator, privacy, fullLevel)
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

type captureCheck struct{ seen *events.Event }

func (*captureCheck) Name() string  { return "test-capture" }
func (*captureCheck) Enabled() bool { return true }
func (c *captureCheck) Check(_ context.Context, event *events.Event) (*security.CheckResult, error) {
	copied := *event
	c.seen = &copied
	return &security.CheckResult{CheckName: "test-capture", Decision: security.DecisionAllow}, nil
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
	_, err := svc.Handle(context.Background(), NewHookRequest("claude-code", event))
	require.NoError(t, err)

	require.NotNil(t, capture.seen)
	assert.Equal(t, "PreToolUse", capture.seen.HookType)
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
	_, err = svc.Handle(ctx, NewHookRequest("claude-code", second))
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

func (f *faultStore) SaveEvent(ctx context.Context, event *events.Event) error {
	if f.failSaveEvent {
		return errors.New("disk full")
	}
	return f.Store.SaveEvent(ctx, event)
}

func TestLocal_Handle_StoreFaults(t *testing.T) {
	cases := []struct {
		name         string
		store        func(t *testing.T) *faultStore
		checks       []security.Check
		wantErr      string
		wantDecision security.Decision
	}{
		{name: "session save race uses the existing session",
			store: func(t *testing.T) *faultStore {
				return &faultStore{Store: storagetest.NewStore(t), saveSessionRaces: true}
			},
			wantDecision: security.DecisionAllow},
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
			wantDecision: security.DecisionBlock},
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
	_, err := svc.Handle(ctx, NewHookRequest("claude-code", event))
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
