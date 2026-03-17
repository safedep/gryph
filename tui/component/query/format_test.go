package query

import (
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func TestEventTarget(t *testing.T) {
	tests := []struct {
		name  string
		event *events.Event
		want  string
	}{
		{
			name:  "file write path",
			event: &events.Event{ActionType: events.ActionFileWrite, Payload: mustJSON(events.FileWritePayload{Path: "/src/main.go"})},
			want:  "/src/main.go",
		},
		{
			name:  "file read path",
			event: &events.Event{ActionType: events.ActionFileRead, Payload: mustJSON(events.FileReadPayload{Path: "/src/utils.go"})},
			want:  "/src/utils.go",
		},
		{
			name:  "file read falls back to pattern",
			event: &events.Event{ActionType: events.ActionFileRead, Payload: mustJSON(events.FileReadPayload{Pattern: "*.go"})},
			want:  "*.go",
		},
		{
			name:  "file delete path",
			event: &events.Event{ActionType: events.ActionFileDelete, Payload: mustJSON(events.FileDeletePayload{Path: "/tmp/old.go"})},
			want:  "/tmp/old.go",
		},
		{
			name:  "command exec",
			event: &events.Event{ActionType: events.ActionCommandExec, Payload: mustJSON(events.CommandExecPayload{Command: "go test ./..."})},
			want:  "go test ./...",
		},
		{
			name:  "tool use",
			event: &events.Event{ActionType: events.ActionToolUse, Payload: mustJSON(events.ToolUsePayload{ToolName: "WebSearch"})},
			want:  "WebSearch",
		},
		{
			name:  "session start",
			event: &events.Event{ActionType: events.ActionSessionStart},
			want:  "started",
		},
		{
			name:  "session end",
			event: &events.Event{ActionType: events.ActionSessionEnd},
			want:  "ended",
		},
		{
			name:  "fallback to tool name",
			event: &events.Event{ActionType: events.ActionUnknown, ToolName: "CustomTool"},
			want:  "CustomTool",
		},
		{
			name:  "empty when no payload and no tool name",
			event: &events.Event{ActionType: events.ActionUnknown},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventTarget(tt.event)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEventDetail(t *testing.T) {
	tests := []struct {
		name  string
		event *events.Event
		want  string
	}{
		{
			name:  "file write line changes",
			event: &events.Event{ActionType: events.ActionFileWrite, Payload: mustJSON(events.FileWritePayload{LinesAdded: 5, LinesRemoved: 2})},
			want:  "+5 -2",
		},
		{
			name:  "file write added only",
			event: &events.Event{ActionType: events.ActionFileWrite, Payload: mustJSON(events.FileWritePayload{LinesAdded: 3, LinesRemoved: 0})},
			want:  "+3",
		},
		{
			name:  "file write removed only",
			event: &events.Event{ActionType: events.ActionFileWrite, Payload: mustJSON(events.FileWritePayload{LinesAdded: 0, LinesRemoved: 4})},
			want:  "-4",
		},
		{
			name:  "file write no changes",
			event: &events.Event{ActionType: events.ActionFileWrite, Payload: mustJSON(events.FileWritePayload{LinesAdded: 0, LinesRemoved: 0})},
			want:  "",
		},
		{
			name:  "command exit code",
			event: &events.Event{ActionType: events.ActionCommandExec, Payload: mustJSON(events.CommandExecPayload{ExitCode: 1})},
			want:  "exit:1",
		},
		{
			name:  "command exit code zero",
			event: &events.Event{ActionType: events.ActionCommandExec, Payload: mustJSON(events.CommandExecPayload{ExitCode: 0})},
			want:  "exit:0",
		},
		{
			name:  "no detail for file read",
			event: &events.Event{ActionType: events.ActionFileRead},
			want:  "",
		},
		{
			name:  "no detail for session start",
			event: &events.Event{ActionType: events.ActionSessionStart},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventDetail(tt.event)
			assert.Equal(t, tt.want, got)
		})
	}
}
