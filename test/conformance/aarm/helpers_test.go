package aarmconformance_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	aarmconf "github.com/safedep/gryph/aarm/conformance"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/require"
)

// fixtureKind discriminates between the two fixture shapes the suite ships:
// "event" is an events.Event JSON document the mediator's adapter will
// normalize, "action" is a pre-canonicalized model.Action that tests feed
// directly to the PDP or to a model.Action sink (used for network_request
// fixtures the hook adapter does not URL-extract for, per spec).
type fixtureKind string

const (
	fixtureEvent  fixtureKind = "event"
	fixtureAction fixtureKind = "action"
)

// fixtureEnvelope is the on-disk JSON shape every fixtures/actions/*.json
// file uses. The kind field selects which embedded shape to interpret.
type fixtureEnvelope struct {
	Kind fixtureKind `json:"kind"`

	// event-shaped fields
	ActionType       events.ActionType `json:"action_type"`
	AgentName        string            `json:"agent_name"`
	ToolName         string            `json:"tool_name,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	SubagentID       string            `json:"subagent_id,omitempty"`
	SubagentType     string            `json:"subagent_type,omitempty"`
	Payload          json.RawMessage   `json:"payload,omitempty"`

	// action-shaped fields (mutually exclusive with the event-shaped fields
	// above except action_type, which both use)
	Agent          string                 `json:"agent,omitempty"`
	Tool           string                 `json:"tool,omitempty"`
	Operation      string                 `json:"operation,omitempty"`
	Project        string                 `json:"project,omitempty"`
	HumanPrincipal string                 `json:"human_principal,omitempty"`
	RoleScope      string                 `json:"role_scope,omitempty"`
	InjectionScore float32                `json:"injection_score,omitempty"`
	Classifications []string              `json:"data_classifications,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
}

// fixturesRoot returns the absolute path to the suite's fixtures directory.
// The conformance test binary may be invoked from a different cwd than the
// package directory; the test always resolves to the package directory
// because the helper uses the test source file's path indirectly via
// runtime.Caller-less filesystem lookups under cwd and parents.
func fixturesRoot(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"fixtures",
		"./test/conformance/aarm/fixtures",
		"../fixtures",
		"../../fixtures",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	cwd, _ := os.Getwd()
	require.Fail(t, "could not locate fixtures dir", "cwd=%s", cwd)
	return ""
}

// loadActionFixture reads a fixture and returns it as a canonical
// model.Action. For event-shaped fixtures, the helper round-trips through
// the production mediation HookAdapter so the resulting action exercises
// the same normalization path the mediator uses.
func loadActionFixture(t *testing.T, name string) *model.Action {
	t.Helper()
	env := readFixture(t, name)
	switch env.Kind {
	case fixtureEvent:
		return actionFromEventFixture(t, env)
	case fixtureAction:
		return actionFromActionFixture(t, env)
	default:
		require.Fail(t, "unknown fixture kind", "fixture %s: kind %q", name, env.Kind)
		return nil
	}
}

// loadEventFixture reads a fixture and returns it as an events.Event. Only
// event-shaped fixtures are convertible. Tests that drive the mediator's
// Check method use this path because the adapter expects an events.Event.
func loadEventFixture(t *testing.T, name string) *events.Event {
	t.Helper()
	env := readFixture(t, name)
	require.Equal(t, fixtureEvent, env.Kind, "fixture %s is not event-shaped", name)
	return eventFromFixture(env)
}

func readFixture(t *testing.T, name string) *fixtureEnvelope {
	t.Helper()
	if filepath.Ext(name) == "" {
		name = name + ".json"
	}
	path := filepath.Join(fixturesRoot(t), "actions", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read fixture %s", path)
	env := &fixtureEnvelope{}
	require.NoError(t, json.Unmarshal(data, env), "parse fixture %s", path)
	if env.Kind == "" {
		env.Kind = fixtureEvent
	}
	return env
}

func eventFromFixture(env *fixtureEnvelope) *events.Event {
	return &events.Event{
		ID:               uuid.New(),
		SessionID:        uuid.New(),
		Timestamp:        time.Now().UTC(),
		ActionType:       env.ActionType,
		AgentName:        env.AgentName,
		ToolName:         env.ToolName,
		WorkingDirectory: env.WorkingDirectory,
		SubagentID:       env.SubagentID,
		SubagentType:     env.SubagentType,
		ResultStatus:     events.ResultSuccess,
		Payload:          env.Payload,
	}
}

func actionFromEventFixture(t *testing.T, env *fixtureEnvelope) *model.Action {
	t.Helper()
	ev := eventFromFixture(env)
	action := &model.Action{
		ID:           uuid.New(),
		Timestamp:    ev.Timestamp,
		SessionID:    ev.SessionID,
		EventID:      ev.ID,
		Type:         model.ActionType(ev.ActionType),
		Tool:         ev.ToolName,
		Agent:        ev.AgentName,
		WorkingDir:   ev.WorkingDirectory,
		SubagentID:   ev.SubagentID,
		SubagentType: ev.SubagentType,
	}
	params, err := extractParamsFromEvent(ev)
	require.NoError(t, err, "extract params for %s", ev.ActionType)
	action.Parameters = params
	return action
}

func actionFromActionFixture(t *testing.T, env *fixtureEnvelope) *model.Action {
	t.Helper()
	action := &model.Action{
		ID:                  uuid.New(),
		Timestamp:           time.Now().UTC(),
		SessionID:           uuid.New(),
		EventID:             uuid.New(),
		Type:                model.ActionType(env.ActionType),
		Tool:                env.Tool,
		Operation:           env.Operation,
		Agent:               env.Agent,
		Project:             env.Project,
		HumanPrincipal:      env.HumanPrincipal,
		RoleScope:           env.RoleScope,
		InjectionScore:      env.InjectionScore,
		DataClassifications: env.Classifications,
	}
	if env.Params != nil {
		if v, ok := env.Params["path"].(string); ok {
			action.Parameters.Path = v
		}
		if v, ok := env.Params["command"].(string); ok {
			action.Parameters.Command = v
		}
		if v, ok := env.Params["url"].(string); ok {
			action.Parameters.URL = v
		}
		if v, ok := env.Params["lines_added"].(float64); ok {
			action.Parameters.LinesAdded = int(v)
		}
		if v, ok := env.Params["lines_removed"].(float64); ok {
			action.Parameters.LinesRemoved = int(v)
		}
		if v, ok := env.Params["size_bytes"].(float64); ok {
			action.Parameters.SizeBytes = int64(v)
		}
		if v, ok := env.Params["content"].(string); ok {
			action.Parameters.Content = v
		}
		if v, ok := env.Params["args"].([]interface{}); ok {
			for _, a := range v {
				if s, ok := a.(string); ok {
					action.Parameters.Args = append(action.Parameters.Args, s)
				}
			}
		}
	}
	return action
}

// extractParamsFromEvent is a local mirror of the hook adapter's parameter
// extraction. Mirroring keeps the helpers package from importing
// aarm/mediation for one private function; the surface area is small and
// the duplication is intentional.
func extractParamsFromEvent(ev *events.Event) (model.Parameters, error) {
	if len(ev.Payload) == 0 {
		return model.Parameters{}, nil
	}
	switch ev.ActionType {
	case events.ActionFileRead:
		p, err := ev.GetFileReadPayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		path := p.Path
		if path == "" {
			path = p.Pattern
		}
		return model.Parameters{Path: path, SizeBytes: p.SizeBytes}, nil
	case events.ActionFileWrite:
		p, err := ev.GetFileWritePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{
			Path:         p.Path,
			SizeBytes:    p.SizeBytes,
			LinesAdded:   p.LinesAdded,
			LinesRemoved: p.LinesRemoved,
			Content:      p.ContentPreview,
		}, nil
	case events.ActionFileDelete:
		p, err := ev.GetFileDeletePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{Path: p.Path}, nil
	case events.ActionCommandExec:
		p, err := ev.GetCommandExecPayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{
			Command: p.Command,
			Args:    p.Args,
			Content: p.StdoutPreview,
		}, nil
	case events.ActionToolUse:
		p, err := ev.GetToolUsePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		var raw map[string]any
		if len(p.Input) > 0 {
			if err := json.Unmarshal(p.Input, &raw); err != nil {
				return model.Parameters{Raw: nil}, nil
			}
		}
		params := model.Parameters{Raw: raw}
		if v, ok := raw["url"].(string); ok {
			params.URL = v
		}
		if v, ok := raw["path"].(string); ok && params.Path == "" {
			params.Path = v
		}
		if v, ok := raw["file_path"].(string); ok && params.Path == "" {
			params.Path = v
		}
		if v, ok := raw["command"].(string); ok && params.Command == "" {
			params.Command = v
		}
		return params, nil
	default:
		return model.Parameters{}, nil
	}
}

// fixturePath returns the absolute path to a named fixture under
// fixtures/<kind>/. Used by tests that need to pass a path to
// conformance.WithPolicy.
func fixturePath(t *testing.T, kind, name string) string {
	t.Helper()
	if filepath.Ext(name) == "" {
		name = name + ".yaml"
	}
	p := filepath.Join(fixturesRoot(t), kind, name)
	_, err := os.Stat(p)
	require.NoError(t, err, "fixture %s", p)
	return p
}

// mustEvaluate drives the reference policy's PDP directly with the supplied
// action and snapshot. Tests that need to assert a specific PDP-only
// decision (e.g. defer, escalate, warn) without the mediator's pre- and
// post-processing use this. The PDP is reconstructed from the bundle's
// policy so it shares the same deferral / session-start configuration the
// mediator uses.
func mustEvaluate(t *testing.T, ref *aarmconf.ReferenceBundle, action *model.Action, snapshot *model.ContextSnapshot) *model.EvaluationResult {
	t.Helper()
	require.NotNil(t, ref)
	require.NotNil(t, ref.Policy)
	engine, err := pdp.New(ref.Policy,
		pdp.WithDeferConfig(pdp.DeferConfig{
			Enabled:               true,
			FreshSessionSeconds:   60,
			ConflictTriggersDefer: true,
		}),
	)
	require.NoError(t, err)
	dec, err := engine.Evaluate(context.Background(), action, snapshot)
	require.NoError(t, err)
	require.NotNil(t, dec)
	return dec
}
