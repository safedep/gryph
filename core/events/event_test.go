package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEvent(t *testing.T) {
	sessionID := uuid.New()
	agentName := "claude-code"
	actionType := ActionCommandExec

	event := NewEvent(sessionID, agentName, actionType)

	require.NotNil(t, event)
	assert.NotEqual(t, uuid.Nil, event.ID)
	assert.Equal(t, sessionID, event.SessionID)
	assert.Equal(t, agentName, event.AgentName)
	assert.Equal(t, actionType, event.ActionType)
	assert.Equal(t, ResultSuccess, event.ResultStatus)
	assert.WithinDuration(t, time.Now().UTC(), event.Timestamp, time.Second)
}

func TestNewEvent_UniqueIDs(t *testing.T) {
	sessionID := uuid.New()

	event1 := NewEvent(sessionID, "agent", ActionFileRead)
	event2 := NewEvent(sessionID, "agent", ActionFileRead)

	assert.NotEqual(t, event1.ID, event2.ID, "Each event should have a unique ID")
}

func TestEvent_SetPayload_CommandExec(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)

	payload := CommandExecPayload{
		Command:     "npm install",
		Description: "Install dependencies",
		ExitCode:    0,
		DurationMs:  1500,
	}

	err := event.SetPayload(payload)
	require.NoError(t, err)
	assert.NotEmpty(t, event.Payload)

	// Verify payload is valid JSON
	var decoded CommandExecPayload
	err = json.Unmarshal(event.Payload, &decoded)
	require.NoError(t, err)
	assert.Equal(t, payload.Command, decoded.Command)
	assert.Equal(t, payload.Description, decoded.Description)
	assert.Equal(t, payload.ExitCode, decoded.ExitCode)
	assert.Equal(t, payload.DurationMs, decoded.DurationMs)
}

func TestEvent_SetPayload_FileRead(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileRead)

	payload := FileReadPayload{
		Path:        "/home/user/project/main.go",
		SizeBytes:   1024,
		ContentHash: "abc123",
	}

	err := event.SetPayload(payload)
	require.NoError(t, err)

	var decoded FileReadPayload
	err = json.Unmarshal(event.Payload, &decoded)
	require.NoError(t, err)
	assert.Equal(t, payload.Path, decoded.Path)
	assert.Equal(t, payload.SizeBytes, decoded.SizeBytes)
	assert.Equal(t, payload.ContentHash, decoded.ContentHash)
}

func TestEvent_SetPayload_FileWrite(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileWrite)

	payload := FileWritePayload{
		Path:           "/home/user/project/main.go",
		SizeBytes:      2048,
		ContentPreview: "package main\n\nfunc main() {}",
		LinesAdded:     10,
		LinesRemoved:   5,
	}

	err := event.SetPayload(payload)
	require.NoError(t, err)

	var decoded FileWritePayload
	err = json.Unmarshal(event.Payload, &decoded)
	require.NoError(t, err)
	assert.Equal(t, payload.Path, decoded.Path)
	assert.Equal(t, payload.SizeBytes, decoded.SizeBytes)
	assert.Equal(t, payload.ContentPreview, decoded.ContentPreview)
	assert.Equal(t, payload.LinesAdded, decoded.LinesAdded)
	assert.Equal(t, payload.LinesRemoved, decoded.LinesRemoved)
}

func TestEvent_GetCommandExecPayload(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)

	payload := CommandExecPayload{
		Command:     "go build",
		Description: "Build project",
		ExitCode:    0,
		Args:        []string{"-o", "bin/app"},
	}
	err := event.SetPayload(payload)
	require.NoError(t, err)

	retrieved, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, payload.Command, retrieved.Command)
	assert.Equal(t, payload.Description, retrieved.Description)
	assert.Equal(t, payload.ExitCode, retrieved.ExitCode)
	assert.Equal(t, payload.Args, retrieved.Args)
}

func TestEvent_GetCommandExecPayload_WrongActionType(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileRead)
	event.Payload = []byte(`{"command": "test"}`)

	retrieved, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Nil(t, retrieved, "Should return nil for wrong action type")
}

func TestEvent_GetFileReadPayload(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileRead)

	payload := FileReadPayload{
		Path:    "/home/user/file.txt",
		Pattern: "*.go",
	}
	err := event.SetPayload(payload)
	require.NoError(t, err)

	retrieved, err := event.GetFileReadPayload()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, payload.Path, retrieved.Path)
	assert.Equal(t, payload.Pattern, retrieved.Pattern)
}

func TestEvent_GetFileReadPayload_WrongActionType(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
	event.Payload = []byte(`{"path": "/test"}`)

	retrieved, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Nil(t, retrieved, "Should return nil for wrong action type")
}

func TestEvent_GetFileWritePayload(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileWrite)

	payload := FileWritePayload{
		Path:       "/home/user/file.txt",
		OldString:  "old content",
		NewString:  "new content",
		LinesAdded: 5,
	}
	err := event.SetPayload(payload)
	require.NoError(t, err)

	retrieved, err := event.GetFileWritePayload()
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, payload.Path, retrieved.Path)
	assert.Equal(t, payload.OldString, retrieved.OldString)
	assert.Equal(t, payload.NewString, retrieved.NewString)
	assert.Equal(t, payload.LinesAdded, retrieved.LinesAdded)
}

func TestEvent_GetFileWritePayload_WrongActionType(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileRead)
	event.Payload = []byte(`{"path": "/test"}`)

	retrieved, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Nil(t, retrieved, "Should return nil for wrong action type")
}

func TestEvent_JSONSerialization(t *testing.T) {
	sessionID := uuid.New()
	event := NewEvent(sessionID, "claude-code", ActionCommandExec)
	event.Sequence = 1
	event.AgentSessionID = "agent-session-123"
	event.WorkingDirectory = "/home/user/project"
	event.ToolName = "Bash"
	event.AgentVersion = "1.0.0"
	event.DurationMs = 500
	event.IsSensitive = false

	payload := CommandExecPayload{Command: "ls -la", ExitCode: 0}
	err := event.SetPayload(payload)
	require.NoError(t, err)

	// Serialize
	data, err := json.Marshal(event)
	require.NoError(t, err)

	// Deserialize
	var decoded Event
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event.ID, decoded.ID)
	assert.Equal(t, event.SessionID, decoded.SessionID)
	assert.Equal(t, event.AgentSessionID, decoded.AgentSessionID)
	assert.Equal(t, event.Sequence, decoded.Sequence)
	assert.Equal(t, event.AgentName, decoded.AgentName)
	assert.Equal(t, event.ActionType, decoded.ActionType)
	assert.Equal(t, event.ResultStatus, decoded.ResultStatus)
	assert.Equal(t, event.WorkingDirectory, decoded.WorkingDirectory)
	assert.Equal(t, event.ToolName, decoded.ToolName)
	assert.Equal(t, event.AgentVersion, decoded.AgentVersion)
	assert.Equal(t, event.DurationMs, decoded.DurationMs)
	assert.Equal(t, event.IsSensitive, decoded.IsSensitive)
}

func TestEvent_PayloadTypes(t *testing.T) {
	testCases := []struct {
		name       string
		actionType ActionType
		payload    interface{}
	}{
		{
			name:       "FileDeletePayload",
			actionType: ActionFileDelete,
			payload:    FileDeletePayload{Path: "/home/user/file.txt"},
		},
		{
			name:       "ToolUsePayload",
			actionType: ActionToolUse,
			payload: ToolUsePayload{
				ToolName:      "WebSearch",
				Input:         json.RawMessage(`{"query": "golang testing"}`),
				OutputPreview: "Search results...",
			},
		},
		{
			name:       "SessionPayload",
			actionType: ActionSessionStart,
			payload: SessionPayload{
				Source:    "cli",
				Model:     "claude-3-opus",
				AgentType: "coding",
			},
		},
		{
			name:       "SessionEndPayload",
			actionType: ActionSessionEnd,
			payload:    SessionEndPayload{Reason: "user_exit"},
		},
		{
			name:       "NotificationPayload",
			actionType: ActionNotification,
			payload: NotificationPayload{
				Message: "Build completed",
				Type:    "info",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := NewEvent(uuid.New(), "claude-code", tc.actionType)
			err := event.SetPayload(tc.payload)
			require.NoError(t, err)
			assert.NotEmpty(t, event.Payload)

			// Verify it's valid JSON
			var raw json.RawMessage
			err = json.Unmarshal(event.Payload, &raw)
			require.NoError(t, err)
		})
	}
}

func TestActionType_String(t *testing.T) {
	testCases := []struct {
		actionType ActionType
		expected   string
	}{
		{ActionFileRead, "file_read"},
		{ActionFileWrite, "file_write"},
		{ActionFileDelete, "file_delete"},
		{ActionCommandExec, "command_exec"},
		{ActionNetworkRequest, "network_request"},
		{ActionToolUse, "tool_use"},
		{ActionSessionStart, "session_start"},
		{ActionSessionEnd, "session_end"},
		{ActionNotification, "notification"},
		{ActionUnknown, "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.actionType.String())
		})
	}
}

func TestActionType_IsValid(t *testing.T) {
	validTypes := []ActionType{
		ActionFileRead, ActionFileWrite, ActionFileDelete,
		ActionCommandExec, ActionNetworkRequest, ActionToolUse,
		ActionSessionStart, ActionSessionEnd, ActionNotification, ActionUnknown,
	}

	for _, at := range validTypes {
		t.Run(at.String(), func(t *testing.T) {
			assert.True(t, at.IsValid())
		})
	}

	// Test invalid type
	invalidType := ActionType("invalid_action")
	assert.False(t, invalidType.IsValid())
}

func TestResultStatus_String(t *testing.T) {
	testCases := []struct {
		status   ResultStatus
		expected string
	}{
		{ResultSuccess, "success"},
		{ResultError, "error"},
		{ResultBlocked, "blocked"},
		{ResultRejected, "rejected"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.status.String())
		})
	}
}

func TestResultStatus_IsValid(t *testing.T) {
	validStatuses := []ResultStatus{
		ResultSuccess, ResultError, ResultBlocked, ResultRejected,
	}

	for _, rs := range validStatuses {
		t.Run(rs.String(), func(t *testing.T) {
			assert.True(t, rs.IsValid())
		})
	}

	// Test invalid status
	invalidStatus := ResultStatus("invalid_status")
	assert.False(t, invalidStatus.IsValid())
}

func TestEvent_ErrorFields(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
	event.ResultStatus = ResultError
	event.ErrorMessage = "Command failed with exit code 1"

	assert.Equal(t, ResultError, event.ResultStatus)
	assert.Equal(t, "Command failed with exit code 1", event.ErrorMessage)
}

func TestEvent_SensitiveFlag(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileRead)
	assert.False(t, event.IsSensitive, "Default should be false")

	event.IsSensitive = true
	assert.True(t, event.IsSensitive)
}

func TestEvent_RawEventStorage(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)

	rawEvent := json.RawMessage(`{"original": "event", "from": "agent"}`)
	event.RawEvent = rawEvent

	assert.Equal(t, rawEvent, event.RawEvent)
}

func TestEvent_DiffContent(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionFileWrite)
	event.DiffContent = "--- a/file.go\n+++ b/file.go\n@@ -1,3 +1,4 @@\n+// new comment"

	assert.Contains(t, event.DiffContent, "---")
	assert.Contains(t, event.DiffContent, "+++")
}

func TestEvent_ConversationContext(t *testing.T) {
	event := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
	event.ConversationContext = "User asked to build the project"

	assert.Equal(t, "User asked to build the project", event.ConversationContext)
}
