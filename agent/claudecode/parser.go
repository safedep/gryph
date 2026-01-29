package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// HookEvent represents the raw event from Claude Code hooks.
type HookEvent struct {
	HookType  string                 `json:"hook_type"`
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	ToolOutput string                `json:"tool_output,omitempty"`
	Session   SessionInfo            `json:"session"`
	Conversation []ConversationMessage `json:"conversation,omitempty"`
}

// SessionInfo contains session information from Claude Code.
type SessionInfo struct {
	ID               string `json:"id"`
	WorkingDirectory string `json:"working_directory"`
}

// ConversationMessage represents a message in the conversation.
type ConversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolNameMapping maps Claude Code tool names to action types.
var ToolNameMapping = map[string]events.ActionType{
	"Read":      events.ActionFileRead,
	"View":      events.ActionFileRead,
	"Write":     events.ActionFileWrite,
	"Edit":      events.ActionFileWrite,
	"Bash":      events.ActionCommandExec,
	"Execute":   events.ActionCommandExec,
	"WebSearch": events.ActionToolUse,
}

// ParseHookEvent converts a Claude Code event to the common format.
func ParseHookEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	var hookEvent HookEvent
	if err := json.Unmarshal(rawData, &hookEvent); err != nil {
		return nil, fmt.Errorf("failed to parse hook event: %w", err)
	}

	// Determine session ID
	var sessionID uuid.UUID
	if hookEvent.Session.ID != "" {
		var err error
		sessionID, err = uuid.Parse(hookEvent.Session.ID)
		if err != nil {
			// Generate a deterministic UUID from the session ID string
			sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(hookEvent.Session.ID))
		}
	} else {
		sessionID = uuid.New()
	}

	// Determine action type
	actionType := events.ActionUnknown
	if at, ok := ToolNameMapping[hookEvent.ToolName]; ok {
		actionType = at
	} else {
		actionType = events.ActionToolUse
	}

	// Create event
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.ToolName = hookEvent.ToolName
	event.WorkingDirectory = hookEvent.Session.WorkingDirectory
	event.RawEvent = rawData

	// Build payload based on action type
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{}
		if path, ok := hookEvent.ToolInput["file_path"].(string); ok {
			payload.Path = path
		}
		event.SetPayload(payload)

	case events.ActionFileWrite:
		payload := events.FileWritePayload{}
		if path, ok := hookEvent.ToolInput["file_path"].(string); ok {
			payload.Path = path
		}
		event.SetPayload(payload)

	case events.ActionCommandExec:
		payload := events.CommandExecPayload{}
		if cmd, ok := hookEvent.ToolInput["command"].(string); ok {
			payload.Command = cmd
		}
		event.SetPayload(payload)

	default:
		payload := events.ToolUsePayload{
			ToolName: hookEvent.ToolName,
		}
		if input, err := json.Marshal(hookEvent.ToolInput); err == nil {
			payload.Input = input
		}
		event.SetPayload(payload)
	}

	// Add conversation context if present
	if len(hookEvent.Conversation) > 0 {
		if convData, err := json.Marshal(hookEvent.Conversation); err == nil {
			event.ConversationContext = string(convData)
		}
	}

	// Detect errors from tool output
	if hookEvent.ToolOutput != "" {
		lowerOutput := strings.ToLower(hookEvent.ToolOutput)
		if strings.Contains(lowerOutput, "error") ||
			strings.Contains(lowerOutput, "failed") ||
			strings.Contains(lowerOutput, "permission denied") ||
			strings.Contains(lowerOutput, "not found") ||
			strings.Contains(lowerOutput, "no such file") {
			event.ResultStatus = events.ResultError
			event.ErrorMessage = truncateString(hookEvent.ToolOutput, 500)
		}
	}

	// Mark sensitive paths using default patterns
	privacyChecker, _ := events.NewPrivacyChecker(events.DefaultSensitivePatterns(), nil)
	if privacyChecker != nil {
		switch actionType {
		case events.ActionFileRead:
			if path, ok := hookEvent.ToolInput["file_path"].(string); ok {
				event.IsSensitive = privacyChecker.IsSensitivePath(path)
			}
		case events.ActionFileWrite:
			if path, ok := hookEvent.ToolInput["file_path"].(string); ok {
				event.IsSensitive = privacyChecker.IsSensitivePath(path)
			}
		}
	}

	return event, nil
}

// truncateString truncates a string to the given max length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
