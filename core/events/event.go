package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents a single action performed by an agent.
type Event struct {
	// ID is the unique identifier for this event.
	ID uuid.UUID `json:"id"`
	// SessionID links this event to its parent session.
	SessionID uuid.UUID `json:"session_id"`
	// Sequence is the order within the session (1, 2, 3...).
	Sequence int `json:"sequence"`
	// Timestamp is when the action occurred (UTC).
	Timestamp time.Time `json:"timestamp"`
	// DurationMs is how long the action took in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`
	// AgentName is the identifier of the agent (e.g., "claude-code").
	AgentName string `json:"agent_name"`
	// AgentVersion is the version of the agent if detectable.
	AgentVersion string `json:"agent_version,omitempty"`
	// WorkingDirectory may differ from session if agent changed dirs.
	WorkingDirectory string `json:"working_directory,omitempty"`
	// ActionType is the category of action performed.
	ActionType ActionType `json:"action_type"`
	// ToolName is the original tool name from the agent.
	ToolName string `json:"tool_name,omitempty"`
	// ResultStatus is the outcome of the action.
	ResultStatus ResultStatus `json:"result_status"`
	// ErrorMessage contains error details if status is error.
	ErrorMessage string `json:"error_message,omitempty"`
	// Payload contains action-specific data.
	Payload json.RawMessage `json:"payload,omitempty"`
	// DiffContent contains file diff (full logging only, never for sensitive paths).
	DiffContent string `json:"diff_content,omitempty"`
	// RawEvent is the original event from agent (full logging only).
	RawEvent json.RawMessage `json:"raw_event,omitempty"`
	// ConversationContext is the prompt/conversation (full logging only).
	ConversationContext string `json:"conversation_context,omitempty"`
	// IsSensitive is true if path matched sensitive_paths pattern.
	IsSensitive bool `json:"is_sensitive"`
}

// NewEvent creates a new Event with a generated UUID and current timestamp.
func NewEvent(sessionID uuid.UUID, agentName string, actionType ActionType) *Event {
	return &Event{
		ID:           uuid.New(),
		SessionID:    sessionID,
		Timestamp:    time.Now().UTC(),
		AgentName:    agentName,
		ActionType:   actionType,
		ResultStatus: ResultSuccess,
	}
}

// FileReadPayload represents the payload for file_read actions.
type FileReadPayload struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// FileWritePayload represents the payload for file_write actions.
type FileWritePayload struct {
	Path         string `json:"path"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	LinesAdded   int    `json:"lines_added,omitempty"`
	LinesRemoved int    `json:"lines_removed,omitempty"`
}

// FileDeletePayload represents the payload for file_delete actions.
type FileDeletePayload struct {
	Path string `json:"path"`
}

// CommandExecPayload represents the payload for command_exec actions.
type CommandExecPayload struct {
	Command       string   `json:"command"`
	Args          []string `json:"args,omitempty"`
	ExitCode      int      `json:"exit_code"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	StdoutPreview string   `json:"stdout_preview,omitempty"`
	StderrPreview string   `json:"stderr_preview,omitempty"`
}

// ToolUsePayload represents the payload for tool_use actions.
type ToolUsePayload struct {
	ToolName      string          `json:"tool_name"`
	Input         json.RawMessage `json:"input,omitempty"`
	OutputPreview string          `json:"output_preview,omitempty"`
}

// SetPayload marshals the given payload and sets it on the event.
func (e *Event) SetPayload(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = data
	return nil
}

// GetFileReadPayload unmarshals the payload as a FileReadPayload.
func (e *Event) GetFileReadPayload() (*FileReadPayload, error) {
	if e.ActionType != ActionFileRead {
		return nil, nil
	}
	var payload FileReadPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetFileWritePayload unmarshals the payload as a FileWritePayload.
func (e *Event) GetFileWritePayload() (*FileWritePayload, error) {
	if e.ActionType != ActionFileWrite {
		return nil, nil
	}
	var payload FileWritePayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetCommandExecPayload unmarshals the payload as a CommandExecPayload.
func (e *Event) GetCommandExecPayload() (*CommandExecPayload, error) {
	if e.ActionType != ActionCommandExec {
		return nil, nil
	}
	var payload CommandExecPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
