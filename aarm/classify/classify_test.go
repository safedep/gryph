package classify

import (
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
)

func TestHeuristic_Classify(t *testing.T) {
	c := NewHeuristic()

	cases := []struct {
		name   string
		action *model.Action
		want   []string
	}{
		{
			name: "env file is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/work/app/.env"},
			},
			want: []string{LabelSecret},
		},
		{
			name: "pem file is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/srv/keys/server.pem"},
			},
			want: []string{LabelSecret},
		},
		{
			name: "ssh dir is secret",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/home/dev/.ssh/id_rsa"},
			},
			want: []string{LabelSecret},
		},
		{
			name: "customer data is pii",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/data/customers/list.csv"},
			},
			want: []string{LabelPII},
		},
		{
			name: "personal report is pii",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/reports/personal_data.json"},
			},
			want: []string{LabelConfig, LabelPII},
		},
		{
			name: "go file is source code",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/cmd/main.go"},
			},
			want: []string{LabelSourceCode},
		},
		{
			name: "yaml file is config",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/k8s/deploy.yaml"},
			},
			want: []string{LabelConfig},
		},
		{
			name: "dockerfile is config",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/Dockerfile"},
			},
			want: []string{LabelConfig},
		},
		{
			name: "git internal",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "/repo/.git/HEAD"},
			},
			want: []string{LabelGitInternal},
		},
		{
			name: "external url",
			action: &model.Action{
				Type:       model.ActionToolUse,
				Parameters: model.Parameters{URL: "https://example.com/api"},
			},
			want: []string{LabelExternalURL},
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
			want: []string{LabelSecret},
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
	c := NewHeuristic(WithExtraPatterns(map[string][]string{
		LabelSecret: {"**/*api_key*"},
		LabelPII:    {"**/customer-list*"},
	}))

	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/repo/internal/api_key.txt"},
	})
	assert.Equal(t, []string{LabelSecret}, got)

	got = c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/repo/data/customer-list-2026.csv"},
	})
	assert.Equal(t, []string{LabelPII}, got)
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
	assert.Equal(t, []string{LabelSecret}, got)
}

func TestNop_Classify(t *testing.T) {
	c := NewNop()
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	})
	assert.Nil(t, got)
}

type stubLabels struct{ labels []string }

func (s stubLabels) Classify(*model.Action) []string { return s.labels }

func TestFailSafe_NilInner(t *testing.T) {
	c := NewFailSafe(nil, LabelUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []string{LabelUnknownSensitive}, got,
		"nil inner Classifier must produce the fail-safe label")
}

func TestFailSafe_EmptyInner(t *testing.T) {
	c := NewFailSafe(stubLabels{labels: nil}, LabelUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []string{LabelUnknownSensitive}, got,
		"inner Classifier returning empty must produce the fail-safe label")
}

func TestFailSafe_InnerHitNotPolluted(t *testing.T) {
	c := NewFailSafe(stubLabels{labels: []string{LabelSecret}}, LabelUnknownSensitive)
	got := c.Classify(&model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/anything"},
	})
	assert.Equal(t, []string{LabelSecret}, got,
		"inner Classifier hits must pass through without the fail-safe label")
}
