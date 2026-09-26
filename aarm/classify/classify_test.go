package classify

import (
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeuristic_Classify(t *testing.T) {
	c := NewHeuristic()

	cases := []struct {
		name   string
		action *model.Action
		want   []privacy.Class
	}{
		{
			name: "env file is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/work/app/.env"},
			},
			want: []privacy.Class{privacy.ClassSecret},
		},
		{
			name: "pem file is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/srv/keys/server.pem"},
			},
			want: []privacy.Class{privacy.ClassSecret},
		},
		{
			name: "ssh dir is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/home/dev/.ssh/id_rsa"},
			},
			want: []privacy.Class{privacy.ClassSecret},
		},
		{
			name: "customer data is pii",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/data/customers/list.csv"},
			},
			want: []privacy.Class{privacy.ClassPII},
		},
		{
			name: "personal report is pii",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/reports/personal_data.json"},
			},
			want: []privacy.Class{privacy.ClassConfig, privacy.ClassPII},
		},
		{
			name: "go file is source code",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/cmd/main.go"},
			},
			want: []privacy.Class{privacy.ClassSourceCode},
		},
		{
			name: "yaml file is config",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/k8s/deploy.yaml"},
			},
			want: []privacy.Class{privacy.ClassConfig},
		},
		{
			name: "dockerfile is config",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/Dockerfile"},
			},
			want: []privacy.Class{privacy.ClassConfig},
		},
		{
			name: "git internal",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/.git/HEAD"},
			},
			want: []privacy.Class{privacy.ClassGitInternal},
		},
		{
			name: "external url",
			action: &model.Action{
				Type:       model.ActionToolUse,
				Parameters: model.Parameters{URL: "https://example.com/api"},
			},
			want: []privacy.Class{privacy.ClassExternalURL},
		},
		{
			name: "localhost url is not external",
			action: &model.Action{
				Type:       model.ActionToolUse,
				Parameters: model.Parameters{URL: "http://localhost:8080/api"},
			},
			want: nil,
		},
		{
			name: "loopback ip is not external",
			action: &model.Action{
				Type:       model.ActionToolUse,
				Parameters: model.Parameters{URL: "http://127.0.0.1/api"},
			},
			want: nil,
		},
		{
			name: "private ip is not external",
			action: &model.Action{
				Type:       model.ActionToolUse,
				Parameters: model.Parameters{URL: "http://10.0.0.1/api"},
			},
			want: nil,
		},
		{
			name: "tool_use file_path raw secret",
			action: &model.Action{
				Type: model.ActionToolUse,
				Parameters: model.Parameters{
					Raw: map[string]any{"file_path": "/var/.env.local"},
				},
			},
			want: []privacy.Class{privacy.ClassSecret},
		},
		{
			name: "unmatched returns nil",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/notes.txt"},
			},
			want: nil,
		},
		{
			name:   "nil action returns nil",
			action: nil,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.Classify(tc.action)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHeuristic_ExtraPatterns(t *testing.T) {
	c := NewHeuristic(WithExtraPatterns(map[privacy.Class][]string{
		privacy.ClassSecret: {"**/*api_key*"},
		privacy.ClassPII:    {"**/customer-list*"},
	}))

	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/repo/internal/api_key.txt"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassSecret}, got)

	got = c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/repo/data/customer-list-2026.csv"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassPII}, got)
}

func TestHeuristic_SecretPathsOverride(t *testing.T) {
	c := NewHeuristic(WithSecretPaths([]string{"**/*.custom-secret"}))

	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/app/.env"},
	})
	assert.Nil(t, got, "with overridden secret paths the default .env match should not fire")

	got = c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/app/db.custom-secret"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassSecret}, got)
}

func TestNop_Classify(t *testing.T) {
	c := NewNop()
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	})
	assert.Nil(t, got)
}

type stubLabels struct{ labels []privacy.Class }

func (s stubLabels) Classify(*model.Action) []privacy.Class { return s.labels }

func TestFailSafe_NilInner(t *testing.T) {
	c := NewFailSafe(nil, privacy.ClassUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassUnknownSensitive}, got,
		"nil inner Classifier must produce the fail-safe label")
}

func TestFailSafe_EmptyInner(t *testing.T) {
	c := NewFailSafe(stubLabels{labels: nil}, privacy.ClassUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassUnknownSensitive}, got,
		"inner Classifier returning empty must produce the fail-safe label")
}

func TestFailSafe_InnerHitNotPolluted(t *testing.T) {
	c := NewFailSafe(stubLabels{labels: []privacy.Class{privacy.ClassSecret}}, privacy.ClassUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []privacy.Class{privacy.ClassSecret}, got,
		"inner Classifier hits must pass through without the fail-safe label")
}

func TestHeuristic_ClassifyEvent(t *testing.T) {
	h := NewHeuristic()
	cases := []struct {
		name    string
		action  events.ActionType
		payload any
		want    []privacy.Class
	}{
		{"file read of a secret", events.ActionFileRead, events.FileReadPayload{Path: "/work/.env"}, []privacy.Class{privacy.ClassSecret}},
		{"file write of source", events.ActionFileWrite, events.FileWritePayload{Path: "/work/main.go"}, []privacy.Class{privacy.ClassSourceCode}},
		{"tool input path and url", events.ActionToolUse, events.ToolUsePayload{
			ToolName: "WebFetch", Input: privacy.NewText(`{"url":"https://example.com/config.yaml"}`),
		}, []privacy.Class{privacy.ClassConfig, privacy.ClassExternalURL}},
		{"command has no path", events.ActionCommandExec, events.CommandExecPayload{Command: privacy.NewText("cat .env")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := events.NewEvent(uuid.New(), "claude-code", tc.action)
			require.NoError(t, e.SetPayload(tc.payload))
			assert.Equal(t, tc.want, h.ClassifyEvent(e))
		})
	}
	assert.Nil(t, (*Heuristic)(nil).ClassifyEvent(events.NewEvent(uuid.New(), "a", events.ActionFileRead)))
}
