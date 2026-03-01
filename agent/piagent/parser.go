package piagent

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

type HookInput struct {
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`
	Timestamp     string `json:"timestamp"`
}

type ToolCallInput struct {
	HookInput
	ToolName   string                 `json:"tool_name"`
	ToolCallID string                 `json:"tool_call_id"`
	Input      map[string]interface{} `json:"input"`
}

type ToolResultInput struct {
	HookInput
	ToolName   string                 `json:"tool_name"`
	ToolCallID string                 `json:"tool_call_id"`
	Input      map[string]interface{} `json:"input"`
	Content    []interface{}          `json:"content"`
	IsError    bool                   `json:"is_error"`
}

type SessionInput struct {
	HookInput
}

var ToolNameMapping = map[string]events.ActionType{
	"read":  events.ActionFileRead,
	"write": events.ActionFileWrite,
	"edit":  events.ActionFileWrite,
	"bash":  events.ActionCommandExec,
	"grep":  events.ActionFileRead,
	"find":  events.ActionFileRead,
	"ls":    events.ActionFileRead,
}

func (a *Adapter) parseHookEvent(hookType string, rawData []byte) (*events.Event, error) {
	var baseInput HookInput
	if err := json.Unmarshal(rawData, &baseInput); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}

	eventName := hookType
	if eventName == "" {
		eventName = baseInput.HookEventName
	}

	sessionID := resolveSessionID(baseInput.SessionID)
	agentSessionID := baseInput.SessionID

	switch eventName {
	case "tool_call":
		return a.parseToolCall(sessionID, agentSessionID, baseInput, rawData)
	case "tool_result":
		return a.parseToolResult(sessionID, agentSessionID, baseInput, rawData)
	case "session_start":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "session_shutdown":
		return parseSessionShutdown(sessionID, agentSessionID, baseInput, rawData)
	default:
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.RawEvent = rawData
		return event, nil
	}
}

func resolveSessionID(rawSessionID string) uuid.UUID {
	if envID := os.Getenv("PI_SESSION_ID"); envID != "" {
		rawSessionID = envID
	}

	if rawSessionID != "" {
		if parsed, err := uuid.Parse(rawSessionID); err == nil {
			return parsed
		}
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte(rawSessionID))
	}

	return uuid.New()
}

func (a *Adapter) parseToolCall(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input ToolCallInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse tool_call input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	if err := a.buildPayload(event, actionType, input.ToolName, input.Input, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.Input)

	return event, nil
}

func (a *Adapter) parseToolResult(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input ToolResultInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse tool_result input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	// Set result status first
	event.ResultStatus = events.ResultSuccess
	if input.IsError {
		event.ResultStatus = events.ResultError
	}

	// Build minimal payload based on action type
	// For file_write and file_read: tool_call already captured the details, just mark success
	// For command exec: capture the output
	toolResponse := make(map[string]interface{})
	toolResponse["content"] = input.Content
	toolResponse["is_error"] = input.IsError

	if err := a.buildPayloadForResult(event, actionType, input.ToolName, input.Input, toolResponse); err != nil {
		return nil, fmt.Errorf("failed to build result payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.Input)

	return event, nil
}

func parseSessionStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse session_start input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionStart)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	payload := events.SessionPayload{
		Source: "startup",
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSessionShutdown(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse session_shutdown input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	payload := events.SessionEndPayload{
		Reason: "shutdown",
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func getActionType(toolName string) events.ActionType {
	if at, ok := ToolNameMapping[toolName]; ok {
		return at
	}
	return events.ActionToolUse
}

func (a *Adapter) buildPayload(event *events.Event, actionType events.ActionType, toolName string, toolInput, toolResponse map[string]interface{}) error {
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{}
		if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
		}
		if pattern, ok := toolInput["pattern"].(string); ok {
			payload.Pattern = pattern
		}
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

	case events.ActionFileWrite:
		payload := events.FileWritePayload{}
		filePath := ""
		if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
			filePath = path
		}

		// Pi Agent uses oldText/newText fields for edits
		fullContent, _ := toolInput["content"].(string)
		fullOldStr, _ := toolInput["oldText"].(string)
		fullNewStr, _ := toolInput["newText"].(string)

		if a.contentHash {
			if fullContent != "" {
				payload.ContentHash = utils.HashContent(fullContent)
			} else if fullOldStr != "" || fullNewStr != "" {
				payload.ContentHash = utils.HashContent(fullOldStr + fullNewStr)
			}
		}

		if fullOldStr != "" || fullNewStr != "" {
			payload.LinesAdded, payload.LinesRemoved = utils.CountDiffLines(fullOldStr, fullNewStr)
		} else {
			oldContent := ""
			if filePath != "" {
				if data, err := os.ReadFile(filePath); err == nil {
					oldContent = string(data)
				}
			}
			if oldContent != "" || fullContent != "" {
				payload.LinesAdded, payload.LinesRemoved = utils.CountDiffLines(oldContent, fullContent)
			}
		}

		if fullContent != "" {
			payload.ContentPreview = truncateString(fullContent, 200)
		}
		if fullOldStr != "" {
			payload.OldString = truncateString(fullOldStr, 200)
		}
		if fullNewStr != "" {
			payload.NewString = truncateString(fullNewStr, 200)
		}

		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

		if a.loggingLevel.IsAtLeast(config.LoggingFull) {
			if fullOldStr != "" || fullNewStr != "" {
				event.DiffContent = utils.GenerateDiff(filePath, fullOldStr, fullNewStr)
			} else if fullContent != "" {
				// Check if file exists to show proper diff (for overwrites)
				oldContent := ""
				if filePath != "" {
					if data, err := os.ReadFile(filePath); err == nil {
						oldContent = string(data)
					}
				}
				event.DiffContent = utils.GenerateDiff(filePath, oldContent, fullContent)
			}
		}

	case events.ActionCommandExec:
		payload := events.CommandExecPayload{}
		if cmd, ok := toolInput["command"].(string); ok {
			payload.Command = cmd
		}
		if desc, ok := toolInput["description"].(string); ok {
			payload.Description = desc
		}
		if toolResponse != nil {
			if content, ok := toolResponse["content"].([]interface{}); ok && len(content) > 0 {
				if textContent, ok := content[0].(map[string]interface{}); ok {
					if text, ok := textContent["text"].(string); ok {
						payload.Output = truncateString(text, 500)
					}
				}
			}
		}
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

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
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}
	}

	return nil
}

// buildPayloadForResult builds minimal payload for tool_result events.
// Unlike tool_call, tool_result should not duplicate the write details
// (lines_added/lines_removed) - those were already captured in tool_call.
// It only captures result-specific data like content read or command output.
func (a *Adapter) buildPayloadForResult(event *events.Event, actionType events.ActionType, toolName string, toolInput, toolResponse map[string]interface{}) error {
	switch actionType {
	case events.ActionFileRead:
		// For reads, tool_call already captured path and pattern
		// tool_result just marks success - no additional payload needed
		payload := events.FileReadPayload{}
		if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
		}
		if pattern, ok := toolInput["pattern"].(string); ok {
			payload.Pattern = pattern
		}
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

	case events.ActionFileWrite:
		// For writes, tool_call already captured the write details
		// Only set minimal path info to link with the tool_call event
		payload := events.FileWritePayload{}
		if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
		}
		// Don't set lines_added/lines_removed - those are in tool_call
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

	case events.ActionCommandExec:
		// Capture output from result
		payload := events.CommandExecPayload{}
		if cmd, ok := toolInput["command"].(string); ok {
			payload.Command = cmd
		}
		if desc, ok := toolInput["description"].(string); ok {
			payload.Description = desc
		}
		if toolResponse != nil {
			if content, ok := toolResponse["content"].([]interface{}); ok && len(content) > 0 {
				if textContent, ok := content[0].(map[string]interface{}); ok {
					if text, ok := textContent["text"].(string); ok {
						payload.Output = truncateString(text, 500)
					}
				}
			}
		}
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}

	default:
		// For unknown tools, use generic tool use payload
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
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}
	}

	return nil
}

func (a *Adapter) markSensitivePaths(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}) {
	if a.privacyChecker == nil {
		return
	}

	switch actionType {
	case events.ActionFileRead, events.ActionFileWrite:
		if path, ok := toolInput["path"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(path)
		}
	case events.ActionCommandExec:
		if cmd, ok := toolInput["command"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(cmd)
		}
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

type HookDecision int

const (
	HookAllow HookDecision = iota
	HookBlock
	HookError
)

type HookResponse struct {
	Decision HookDecision
	Message  string
}

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

func (r *HookResponse) Stderr() string {
	if r.Decision == HookBlock || r.Decision == HookError {
		return r.Message
	}
	return ""
}

type hookResponseJSON struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

func (r *HookResponse) JSON() []byte {
	resp := hookResponseJSON{Decision: "allow"}
	if r.Decision == HookBlock {
		resp.Decision = "block"
		resp.Reason = r.Message
	}
	data, _ := json.Marshal(resp)
	return data
}

func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

func NewBlockResponse(message string) *HookResponse {
	return &HookResponse{
		Decision: HookBlock,
		Message:  message,
	}
}

func NewErrorResponse(message string) *HookResponse {
	return &HookResponse{
		Decision: HookError,
		Message:  message,
	}
}
