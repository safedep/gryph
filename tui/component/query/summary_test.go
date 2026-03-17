package query

import (
	"encoding/json"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestComputeSummary(t *testing.T) {
	tests := []struct {
		name          string
		events        []*events.Event
		wantFiles     int
		wantRead      int
		wantCmds      int
		wantFailed    int
		wantErrors    int
		wantSensitive int
		wantBlocked   int
	}{
		{
			name:   "empty events",
			events: nil,
		},
		{
			name: "file operations",
			events: []*events.Event{
				{ActionType: events.ActionFileRead, ResultStatus: events.ResultSuccess},
				{
					ActionType:   events.ActionFileWrite,
					ResultStatus: events.ResultSuccess,
					Payload:      mustJSON(events.FileWritePayload{Path: "/a.go", LinesAdded: 10, LinesRemoved: 3}),
				},
				{ActionType: events.ActionFileDelete, ResultStatus: events.ResultSuccess},
			},
			wantFiles: 1,
			wantRead:  1,
		},
		{
			name: "commands with failures",
			events: []*events.Event{
				{
					ActionType:   events.ActionCommandExec,
					ResultStatus: events.ResultSuccess,
					Payload:      mustJSON(events.CommandExecPayload{Command: "go test", ExitCode: 0}),
				},
				{
					ActionType:   events.ActionCommandExec,
					ResultStatus: events.ResultSuccess,
					Payload:      mustJSON(events.CommandExecPayload{Command: "go build", ExitCode: 1}),
				},
			},
			wantCmds:   2,
			wantFailed: 1,
		},
		{
			name: "error and sensitive flags",
			events: []*events.Event{
				{ActionType: events.ActionFileRead, ResultStatus: events.ResultError, IsSensitive: true},
				{ActionType: events.ActionFileWrite, ResultStatus: events.ResultBlocked},
			},
			wantRead:      1,
			wantFiles:     1,
			wantErrors:    1,
			wantSensitive: 1,
			wantBlocked:   1,
		},
		{
			name: "rejected counts as blocked",
			events: []*events.Event{
				{ActionType: events.ActionCommandExec, ResultStatus: events.ResultRejected,
					Payload: mustJSON(events.CommandExecPayload{Command: "rm -rf /", ExitCode: 0})},
			},
			wantCmds:    1,
			wantBlocked: 1,
		},
		{
			name: "multiple sensitive events",
			events: []*events.Event{
				{ActionType: events.ActionFileRead, ResultStatus: events.ResultSuccess, IsSensitive: true},
				{ActionType: events.ActionFileRead, ResultStatus: events.ResultSuccess, IsSensitive: true},
				{ActionType: events.ActionFileRead, ResultStatus: events.ResultSuccess, IsSensitive: false},
			},
			wantRead:      3,
			wantSensitive: 2,
		},
		{
			name: "file write without payload",
			events: []*events.Event{
				{ActionType: events.ActionFileWrite, ResultStatus: events.ResultSuccess},
			},
			wantFiles: 1,
		},
		{
			name: "command without payload",
			events: []*events.Event{
				{ActionType: events.ActionCommandExec, ResultStatus: events.ResultSuccess},
			},
			wantCmds: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := computeSummary(tt.events)
			assert.Equal(t, tt.wantFiles, len(s.filesWritten))
			assert.Equal(t, tt.wantRead, s.filesRead)
			assert.Equal(t, tt.wantCmds, len(s.commands))
			assert.Equal(t, tt.wantFailed, s.commandsFailed)
			assert.Equal(t, tt.wantErrors, s.errors)
			assert.Equal(t, tt.wantSensitive, s.sensitive)
			assert.Equal(t, tt.wantBlocked, s.blocked)
		})
	}
}
