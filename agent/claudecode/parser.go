package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// HookInput represents the common fields in all Claude Code hook inputs.
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
}

// PreToolUseInput represents the input for PreToolUse hooks.
type PreToolUseInput struct {
	HookInput
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	ToolUseID string                 `json:"tool_use_id"`
}

// PostToolUseInput represents the input for PostToolUse and PostToolUseFailure hooks.
type PostToolUseInput struct {
	HookInput
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolResponse map[string]interface{} `json:"tool_response"`
	ToolUseID    string                 `json:"tool_use_id"`
}

// SessionStartInput represents the input for SessionStart hooks.
type SessionStartInput struct {
	HookInput
	Source    string `json:"source"`
	Model     string `json:"model"`
	AgentType string `json:"agent_type,omitempty"`
}

// SessionEndInput represents the input for SessionEnd hooks.
type SessionEndInput struct {
	HookInput
	Reason string `json:"reason"`
}

// NotificationInput represents the input for Notification hooks.
type NotificationInput struct {
	HookInput
	Message          string `json:"message"`
	NotificationType string `json:"notification_type"`
}

// ToolNameMapping maps Claude Code tool names to action types.
var ToolNameMapping = map[string]events.ActionType{
	"Read":          events.ActionFileRead,
	"View":          events.ActionFileRead,
	"Write":         events.ActionFileWrite,
	"Edit":          events.ActionFileWrite,
	"NotebookEdit":  events.ActionFileWrite,
	"Bash":          events.ActionCommandExec,
	"Execute":       events.ActionCommandExec,
	"WebSearch":     events.ActionToolUse,
	"WebFetch":      events.ActionToolUse,
	"Grep":          events.ActionFileRead,
	"Glob":          events.ActionFileRead,
	"LS":            events.ActionFileRead,
	"Task":          events.ActionToolUse,
	"TodoRead":      events.ActionToolUse,
	"TodoWrite":     events.ActionToolUse,
	"AskUser":       events.ActionToolUse,
}

// ParseHookEvent converts a Claude Code event to the common format.
func ParseHookEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	// First parse the common fields to determine event type
	var baseInput HookInput
	if err := json.Unmarshal(rawData, &baseInput); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}

	// Use hook_event_name from input if available, otherwise use passed hookType
	eventName := baseInput.HookEventName
	if eventName == "" {
		eventName = hookType
	}

	// Parse session ID
	var sessionID uuid.UUID
	if baseInput.SessionID != "" {
		var err error
		sessionID, err = uuid.Parse(baseInput.SessionID)
		if err != nil {
			// Generate a deterministic UUID from the session ID string
			sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(baseInput.SessionID))
		}
	} else {
		sessionID = uuid.New()
	}

	// Store original agent session ID for correlation
	agentSessionID := baseInput.SessionID

	// Handle different event types
	switch eventName {
	case "PreToolUse":
		return parsePreToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "PostToolUse":
		return parsePostToolUse(sessionID, agentSessionID, baseInput, rawData, false)
	case "PostToolUseFailure":
		return parsePostToolUse(sessionID, agentSessionID, baseInput, rawData, true)
	case "SessionStart":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "SessionEnd":
		return parseSessionEnd(sessionID, agentSessionID, baseInput, rawData)
	case "Notification":
		return parseNotification(sessionID, agentSessionID, baseInput, rawData)
	default:
		// Unknown event type - create a generic event
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.RawEvent = rawData
		return event, nil
	}
}

func parsePreToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PreToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PreToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	// Build payload based on action type
	buildPayload(event, actionType, input.ToolName, input.ToolInput, nil)

	// Mark sensitive paths
	markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func parsePostToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte, isFailure bool) (*events.Event, error) {
	var input PostToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PostToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	// Build payload
	buildPayload(event, actionType, input.ToolName, input.ToolInput, input.ToolResponse)

	// Set result status based on failure flag and response
	if isFailure {
		event.ResultStatus = events.ResultError
		if errMsg, ok := input.ToolResponse["error"].(string); ok {
			event.ErrorMessage = truncateString(errMsg, 500)
		}
	} else {
		event.ResultStatus = events.ResultSuccess
		// Check for errors in response
		detectErrorsInResponse(event, input.ToolResponse)
	}

	// Mark sensitive paths
	markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func parseSessionStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionStartInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse SessionStart input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionStart)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	// Store session metadata in payload
	payload := events.SessionPayload{
		Source:    input.Source,
		Model:     input.Model,
		AgentType: input.AgentType,
	}
	event.SetPayload(payload)

	return event, nil
}

func parseSessionEnd(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionEndInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse SessionEnd input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	// Store reason in payload
	payload := events.SessionEndPayload{
		Reason: input.Reason,
	}
	event.SetPayload(payload)

	return event, nil
}

func parseNotification(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input NotificationInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse Notification input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionNotification)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	payload := events.NotificationPayload{
		Message: input.Message,
		Type:    input.NotificationType,
	}
	event.SetPayload(payload)

	return event, nil
}

func getActionType(toolName string) events.ActionType {
	if at, ok := ToolNameMapping[toolName]; ok {
		return at
	}
	return events.ActionToolUse
}

func buildPayload(event *events.Event, actionType events.ActionType, toolName string, toolInput, toolResponse map[string]interface{}) {
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{}
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
		}
		if pattern, ok := toolInput["pattern"].(string); ok {
			payload.Pattern = pattern
		}
		event.SetPayload(payload)

	case events.ActionFileWrite:
		payload := events.FileWritePayload{}
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
		}
		if content, ok := toolInput["content"].(string); ok {
			payload.ContentPreview = truncateString(content, 200)
		}
		if oldStr, ok := toolInput["old_string"].(string); ok {
			payload.OldString = truncateString(oldStr, 200)
		}
		if newStr, ok := toolInput["new_string"].(string); ok {
			payload.NewString = truncateString(newStr, 200)
		}
		event.SetPayload(payload)

	case events.ActionCommandExec:
		payload := events.CommandExecPayload{}
		if cmd, ok := toolInput["command"].(string); ok {
			payload.Command = cmd
		}
		if desc, ok := toolInput["description"].(string); ok {
			payload.Description = desc
		}
		if toolResponse != nil {
			if output, ok := toolResponse["output"].(string); ok {
				payload.Output = truncateString(output, 500)
			}
			if exitCode, ok := toolResponse["exitCode"].(float64); ok {
				payload.ExitCode = int(exitCode)
			}
		}
		event.SetPayload(payload)

	default:
		payload := events.ToolUsePayload{
			ToolName: toolName,
		}
		if input, err := json.Marshal(toolInput); err == nil {
			payload.Input = input
		}
		if toolResponse != nil {
			if resp, err := json.Marshal(toolResponse); err == nil {
				payload.Output = resp
			}
		}
		event.SetPayload(payload)
	}
}

func detectErrorsInResponse(event *events.Event, response map[string]interface{}) {
	if response == nil {
		return
	}

	// Check for explicit error field
	if errMsg, ok := response["error"].(string); ok && errMsg != "" {
		event.ResultStatus = events.ResultError
		event.ErrorMessage = truncateString(errMsg, 500)
		return
	}

	// Check for success=false
	if success, ok := response["success"].(bool); ok && !success {
		event.ResultStatus = events.ResultError
		return
	}

	// Check output for error patterns
	if output, ok := response["output"].(string); ok {
		lowerOutput := strings.ToLower(output)
		if strings.Contains(lowerOutput, "error:") ||
			strings.Contains(lowerOutput, "failed:") ||
			strings.Contains(lowerOutput, "permission denied") ||
			strings.Contains(lowerOutput, "command not found") ||
			strings.Contains(lowerOutput, "no such file") {
			event.ResultStatus = events.ResultError
			event.ErrorMessage = truncateString(output, 500)
		}
	}
}

func markSensitivePaths(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}) {
	privacyChecker, _ := events.NewPrivacyChecker(events.DefaultSensitivePatterns(), nil)
	if privacyChecker == nil {
		return
	}

	switch actionType {
	case events.ActionFileRead, events.ActionFileWrite:
		if path, ok := toolInput["file_path"].(string); ok {
			event.IsSensitive = privacyChecker.IsSensitivePath(path)
		}
	case events.ActionCommandExec:
		if cmd, ok := toolInput["command"].(string); ok {
			// Check if command accesses sensitive paths
			for _, pattern := range events.DefaultSensitivePatterns() {
				if strings.Contains(cmd, pattern) {
					event.IsSensitive = true
					break
				}
			}
		}
	}
}

// truncateString truncates a string to the given max length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// HookDecision represents the decision for a Claude Code hook.
type HookDecision int

const (
	// HookAllow allows the action to proceed (exit code 0).
	HookAllow HookDecision = iota
	// HookBlock blocks the action (exit code 2, message to stderr, shown to Claude).
	HookBlock
	// HookError is a non-blocking error (exit code 1, message to stderr, shown to user in verbose mode).
	HookError
)

// HookResponse represents a response to Claude Code hooks.
type HookResponse struct {
	// Decision is whether to allow, block, or report error.
	Decision HookDecision
	// Message is the reason (used for HookBlock and HookError).
	Message string
}

// ExitCode returns the exit code for this response.
// Exit code 0 = allow, exit code 2 = block, exit code 1 = non-blocking error.
func (r *HookResponse) ExitCode() int {
	switch r.Decision {
	case HookBlock:
		return 2
	case HookError:
		return 1
	default:
		return 0
	}
}

// Stderr returns the message to write to stderr (for blocking and error).
func (r *HookResponse) Stderr() string {
	if r.Decision == HookBlock || r.Decision == HookError {
		return r.Message
	}
	return ""
}

// NewAllowResponse creates a response that allows the action.
func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

// NewBlockResponse creates a response that blocks the action with a reason.
// The message is shown to Claude.
func NewBlockResponse(message string) *HookResponse {
	return &HookResponse{
		Decision: HookBlock,
		Message:  message,
	}
}

// NewErrorResponse creates a non-blocking error response.
// The message is shown to the user in verbose mode, execution continues.
func NewErrorResponse(message string) *HookResponse {
	return &HookResponse{
		Decision: HookError,
		Message:  message,
	}
}
