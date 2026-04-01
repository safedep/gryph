package codex

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
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Model          string `json:"model"`
}

type PreToolUseInput struct {
	HookInput
	TurnID    string                 `json:"turn_id"`
	ToolName  string                 `json:"tool_name"`
	ToolUseID string                 `json:"tool_use_id"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

type PostToolUseInput struct {
	HookInput
	TurnID       string                 `json:"turn_id"`
	ToolName     string                 `json:"tool_name"`
	ToolUseID    string                 `json:"tool_use_id"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolResponse json.RawMessage        `json:"tool_response"`
}

type SessionStartInput struct {
	HookInput
	Source string `json:"source"`
}

type UserPromptSubmitInput struct {
	HookInput
	TurnID string `json:"turn_id"`
	Prompt string `json:"prompt"`
}

type StopInput struct {
	HookInput
	TurnID               string `json:"turn_id"`
	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

var ToolNameMapping = map[string]events.ActionType{
	"Read":         events.ActionFileRead,
	"View":         events.ActionFileRead,
	"Write":        events.ActionFileWrite,
	"Edit":         events.ActionFileWrite,
	"NotebookEdit": events.ActionFileWrite,
	"Bash":         events.ActionCommandExec,
	"Execute":      events.ActionCommandExec,
	"WebSearch":    events.ActionToolUse,
	"WebFetch":     events.ActionToolUse,
	"Grep":         events.ActionFileRead,
	"Glob":         events.ActionFileRead,
	"LS":           events.ActionFileRead,
	"Task":         events.ActionToolUse,
	"TodoRead":     events.ActionToolUse,
	"TodoWrite":    events.ActionToolUse,
	"AskUser":      events.ActionToolUse,
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
	case "SessionStart":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "PreToolUse":
		return a.parsePreToolUse(sessionID, agentSessionID, rawData)
	case "PostToolUse":
		return a.parsePostToolUse(sessionID, agentSessionID, rawData)
	case "UserPromptSubmit":
		return parseUserPromptSubmit(sessionID, agentSessionID, rawData)
	case "Stop":
		return parseStop(sessionID, agentSessionID, rawData)
	default:
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.TranscriptPath = baseInput.TranscriptPath
		event.RawEvent = rawData
		return event, nil
	}
}

func resolveSessionID(rawSessionID string) uuid.UUID {
	if envID := os.Getenv("CODEX_SESSION_ID"); envID != "" {
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

func parseSessionStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionStartInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse SessionStart input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionStart)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.SessionPayload{
		Source: input.Source,
		Model:  input.Model,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) parsePreToolUse(sessionID uuid.UUID, agentSessionID string, rawData []byte) (*events.Event, error) {
	var input PreToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PreToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	if err := a.buildToolPayload(event, actionType, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func (a *Adapter) parsePostToolUse(sessionID uuid.UUID, agentSessionID string, rawData []byte) (*events.Event, error) {
	var input PostToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PostToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData
	event.ResultStatus = events.ResultSuccess

	var toolResponse interface{}
	if len(input.ToolResponse) > 0 {
		var responseStr string
		if err := json.Unmarshal(input.ToolResponse, &responseStr); err == nil {
			toolResponse = responseStr
		} else {
			var structured interface{}
			if err := json.Unmarshal(input.ToolResponse, &structured); err == nil {
				toolResponse = structured
			}
		}
	}
	if err := a.buildToolPayload(event, actionType, input.ToolInput, toolResponse); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func parseUserPromptSubmit(sessionID uuid.UUID, agentSessionID string, rawData []byte) (*events.Event, error) {
	var input UserPromptSubmitInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse UserPromptSubmit input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "UserPromptSubmit"
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.ToolUsePayload{
		ToolName: "UserPromptSubmit",
	}

	promptInput := map[string]string{"prompt": input.Prompt}
	if data, err := json.Marshal(promptInput); err == nil {
		payload.Input = data
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseStop(sessionID uuid.UUID, agentSessionID string, rawData []byte) (*events.Event, error) {
	var input StopInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse Stop input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.SessionEndPayload{
		Reason: input.LastAssistantMessage,
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

func (a *Adapter) buildToolPayload(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}, toolResponse interface{}) error {
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{}
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
		} else if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
		}
		if pattern, ok := toolInput["pattern"].(string); ok {
			payload.Pattern = pattern
		}
		return event.SetPayload(payload)

	case events.ActionFileWrite:
		payload := events.FileWritePayload{}
		filePath := ""
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
			filePath = path
		}

		fullOldStr, _ := toolInput["old_string"].(string)
		fullNewStr, _ := toolInput["new_string"].(string)
		fullContent, _ := toolInput["content"].(string)

		if a.contentHash {
			if fullContent != "" {
				payload.ContentHash = utils.HashContent(fullContent)
			} else if fullOldStr != "" || fullNewStr != "" {
				payload.ContentHash = utils.HashContent(fullOldStr + fullNewStr)
			}
		}

		if fullOldStr != "" || fullNewStr != "" {
			payload.LinesAdded, payload.LinesRemoved = utils.CountDiffLines(fullOldStr, fullNewStr)
		} else if fullContent != "" {
			payload.LinesAdded = utils.CountNewFileLines(fullContent)
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
			return err
		}

		if a.loggingLevel.IsAtLeast(config.LoggingFull) {
			if fullOldStr != "" || fullNewStr != "" {
				event.DiffContent = utils.GenerateDiff(filePath, fullOldStr, fullNewStr)
			} else if fullContent != "" {
				event.DiffContent = utils.GenerateDiff(filePath, "", fullContent)
			}
		}
		return nil

	case events.ActionCommandExec:
		payload := events.CommandExecPayload{}
		if cmd, ok := toolInput["command"].(string); ok {
			payload.Command = cmd
		}
		if responseStr, ok := toolResponse.(string); ok {
			payload.Output = truncateString(responseStr, 500)
		}
		return event.SetPayload(payload)

	default:
		payload := events.ToolUsePayload{
			ToolName: event.ToolName,
		}
		if input, err := json.Marshal(toolInput); err == nil {
			payload.Input = input
		}
		if toolResponse != nil {
			if resp, err := json.Marshal(toolResponse); err == nil {
				payload.Output = resp
			}
		}
		return event.SetPayload(payload)
	}
}

func (a *Adapter) markSensitivePaths(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}) {
	if a.privacyChecker == nil {
		return
	}

	switch actionType {
	case events.ActionFileRead, events.ActionFileWrite:
		if path, ok := toolInput["file_path"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(path)
		} else if path, ok := toolInput["path"].(string); ok {
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

type HookResponse struct {
	Decision HookDecision
	Message  string
}

type HookDecision int

const (
	HookAllow HookDecision = iota
	HookBlock
	HookError
)

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

type preToolUseOutput struct {
	HookSpecificOutput preToolUseDecision `json:"hookSpecificOutput"`
}

type preToolUseDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

func (r *HookResponse) JSON() []byte {
	output := preToolUseOutput{
		HookSpecificOutput: preToolUseDecision{
			HookEventName: "PreToolUse",
		},
	}

	switch r.Decision {
	case HookBlock:
		output.HookSpecificOutput.PermissionDecision = "deny"
		output.HookSpecificOutput.PermissionDecisionReason = r.Message
	default:
		output.HookSpecificOutput.PermissionDecision = "allow"
	}

	data, _ := json.Marshal(output)
	return data
}

func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

func NewBlockResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookBlock, Message: message}
}

func NewErrorResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookError, Message: message}
}
