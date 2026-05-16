package mediation

import (
	"context"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPAdapterNormalizesFileRead(t *testing.T) {
	a := NewMCPAdapter()
	req := &MCPToolCall{
		Method: "tools/call",
		Params: MCPToolCallParams{
			Name: "file_read",
			Arguments: map[string]any{
				"file_path": "/etc/passwd",
			},
		},
		Meta: map[string]any{"sessionID": "agent-abc"},
	}
	action, err := a.Normalize(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, model.ActionToolUse, action.Type)
	assert.Equal(t, "file_read", action.Tool)
	assert.Equal(t, "/etc/passwd", action.Parameters.Path)
	assert.Equal(t, "agent-abc", action.AgentSessionID)
}

func TestMCPAdapterNormalizesBashExec(t *testing.T) {
	a := NewMCPAdapter()
	req := &MCPToolCall{
		Params: MCPToolCallParams{
			Name: "bash",
			Arguments: map[string]any{
				"command": "rm -rf /",
			},
		},
	}
	action, err := a.Normalize(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, "bash", action.Tool)
	assert.Equal(t, "rm -rf /", action.Parameters.Command)
}

func TestMCPAdapterNormalizesFetch(t *testing.T) {
	a := NewMCPAdapter()
	req := &MCPToolCall{
		Params: MCPToolCallParams{
			Name: "fetch",
			Arguments: map[string]any{
				"url": "https://attacker.example.com/x",
			},
		},
	}
	action, err := a.Normalize(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, "fetch", action.Tool)
	assert.Equal(t, "https://attacker.example.com/x", action.Parameters.URL)
}

func TestMCPAdapterNormalizesPathFallback(t *testing.T) {
	a := NewMCPAdapter()
	req := &MCPToolCall{
		Params: MCPToolCallParams{
			Name: "search",
			Arguments: map[string]any{
				"path": "/srv/code",
			},
		},
	}
	action, err := a.Normalize(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, "/srv/code", action.Parameters.Path)
}

func TestMCPAdapterErrorsOnMissingName(t *testing.T) {
	a := NewMCPAdapter()
	_, err := a.Normalize(context.Background(), &MCPToolCall{}, nil)
	assert.Error(t, err)
}

func TestMCPAdapterErrorsOnNil(t *testing.T) {
	a := NewMCPAdapter()
	_, err := a.Normalize(context.Background(), nil, nil)
	assert.Error(t, err)
}

func TestMCPAdapterSessionFallback(t *testing.T) {
	a := NewMCPAdapter()
	req := &MCPToolCall{
		Params: MCPToolCallParams{Name: "tool"},
	}
	sess := &session.Session{
		AgentSessionID:   "sess-1",
		WorkingDirectory: "/wd",
		ProjectName:      "demo",
	}
	action, err := a.Normalize(context.Background(), req, sess)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", action.AgentSessionID)
	assert.Equal(t, "/wd", action.WorkingDir)
	assert.Equal(t, "demo", action.Project)
}

type fakeClassifier struct{ labels []string }

func (f fakeClassifier) Classify(_ *model.Action) []string { return f.labels }

type fakeScorer struct{ score float32 }

func (f fakeScorer) Score(_ *model.Action) float32 { return f.score }

func TestMCPAdapterRunsClassifierAndScorer(t *testing.T) {
	a := NewMCPAdapter(
		WithMCPClassifier(fakeClassifier{labels: []string{"secret"}}),
		WithMCPInjectionScorer(fakeScorer{score: 0.42}),
	)
	req := &MCPToolCall{
		Params: MCPToolCallParams{Name: "tool", Arguments: map[string]any{"x": 1}},
	}
	action, err := a.Normalize(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"secret"}, action.DataClassifications)
	assert.InDelta(t, 0.42, action.InjectionScore, 1e-6)
}
