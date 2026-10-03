package decision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedHookError struct {
	agent   string
	details map[string]any
	message string
}

func TestLocal_ReportHookError(t *testing.T) {
	redactor, err := privacy.NewRedactor(nil, privacy.DefaultRedactPatterns())
	require.NoError(t, err)
	raw := []byte(`token=hunter2secret {"x":"` + strings.Repeat("a", 70*1024) + `"}`)

	tests := []struct {
		name        string
		level       config.LoggingLevel
		wantRaw     bool
		wantCapped  bool
		wantRedacts bool
	}{
		{name: "full level keeps a capped and redacted payload", level: config.LoggingFull, wantRaw: true, wantCapped: true, wantRedacts: true},
		{name: "standard level keeps no payload", level: config.LoggingStandard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *recordedHookError
			svc := NewLocal(nil, security.New(nil), redactor, func(string) config.LoggingLevel { return tt.level },
				WithHookErrorRecorder(func(_ context.Context, agent string, details map[string]any, msg string) error {
					got = &recordedHookError{agent: agent, details: details, message: msg}
					return nil
				}))

			err := svc.ReportHookError(context.Background(), &HookError{
				Agent: "claude-code", HookType: "PreToolUse", RawSize: len(raw), RawEvent: raw, Message: "failed to parse event",
			})
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "claude-code", got.agent)
			assert.Equal(t, "failed to parse event", got.message)
			assert.Equal(t, "PreToolUse", got.details["hook_type"])
			assert.Equal(t, len(raw), got.details["raw_data_size"])

			text, ok := got.details["raw_event"].(string)
			assert.Equal(t, tt.wantRaw, ok)
			if tt.wantCapped {
				assert.LessOrEqual(t, len(text), maxRawEventSize+64, "the redaction may change the length a little")
			}
			if tt.wantRedacts {
				assert.NotContains(t, text, "hunter2secret")
			}
		})
	}
}

func TestLocal_ReportHookError_NoRecorder(t *testing.T) {
	svc := NewLocal(nil, security.New(nil), nil, fullLevel)
	assert.NoError(t, svc.ReportHookError(context.Background(), &HookError{Agent: "codex"}))
	assert.NoError(t, svc.ReportHookError(context.Background(), nil))
}

func TestLocal_ReportHookError_RecorderError(t *testing.T) {
	svc := NewLocal(nil, security.New(nil), nil, fullLevel,
		WithHookErrorRecorder(func(context.Context, string, map[string]any, string) error { return errors.New("disk full") }))
	assert.EqualError(t, svc.ReportHookError(context.Background(), &HookError{Agent: "codex"}), "disk full")
}
