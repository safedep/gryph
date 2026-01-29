package cursor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// HookEvent represents the raw event from Cursor hooks.
type HookEvent struct {
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	Content        string   `json:"content,omitempty"`
	FilePath       string   `json:"file_path,omitempty"`
	HookEventName  string   `json:"hook_event_name"`
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
}

// HookTypeMapping maps Cursor hook types to action types.
var HookTypeMapping = map[string]events.ActionType{
	"beforeReadFile":       events.ActionFileRead,
	"afterFileEdit":        events.ActionFileWrite,
	"beforeShellExecution": events.ActionCommandExec,
	"beforeMCPExecution":   events.ActionToolUse,
	"beforeSubmitPrompt":   events.ActionToolUse, // Session context
	"stop":                 events.ActionToolUse, // Session end marker
}

// ParseHookEvent converts a Cursor event to the common format.
func ParseHookEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	var hookEvent HookEvent
	if err := json.Unmarshal(rawData, &hookEvent); err != nil {
		return nil, fmt.Errorf("failed to parse hook event: %w", err)
	}

	// Determine session ID from conversation_id
	var sessionID uuid.UUID
	if hookEvent.ConversationID != "" {
		var err error
		sessionID, err = uuid.Parse(hookEvent.ConversationID)
		if err != nil {
			// Generate a deterministic UUID from the conversation ID string
			sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(hookEvent.ConversationID))
		}
	} else {
		sessionID = uuid.New()
	}

	// Determine action type
	actionType := events.ActionUnknown
	if at, ok := HookTypeMapping[hookType]; ok {
		actionType = at
	}

	// Create event
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.ToolName = hookType
	event.RawEvent = rawData

	// Set working directory from workspace roots
	if len(hookEvent.WorkspaceRoots) > 0 {
		event.WorkingDirectory = hookEvent.WorkspaceRoots[0]
	}

	// Build payload based on action type
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{
			Path: hookEvent.FilePath,
		}
		event.SetPayload(payload)

	case events.ActionFileWrite:
		payload := events.FileWritePayload{
			Path: hookEvent.FilePath,
		}
		// Content could contain the diff for afterFileEdit
		if hookEvent.Content != "" {
			event.DiffContent = hookEvent.Content
		}
		event.SetPayload(payload)

	case events.ActionCommandExec:
		payload := events.CommandExecPayload{
			Command: hookEvent.Content,
		}
		event.SetPayload(payload)

	default:
		payload := events.ToolUsePayload{
			ToolName: hookType,
		}
		event.SetPayload(payload)
	}

	return event, nil
}

// GenerateResponse generates a response to send back to Cursor.
// For MVP, this always allows the action.
func GenerateResponse(allow bool, message string) []byte {
	response := map[string]interface{}{
		"status": "allow",
	}
	if !allow {
		response["status"] = "deny"
		response["message"] = message
	}
	data, _ := json.Marshal(response)
	return data
}
