package gemini

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	Timestamp      string `json:"timestamp"`
}

type BeforeToolInput struct {
	HookInput
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

type AfterToolInput struct {
	HookInput
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolResponse map[string]interface{} `json:"tool_response"`
}

type SessionStartInput struct {
	HookInput
	Source string `json:"source"`
}

type SessionEndInput struct {
	HookInput
	Reason string `json:"reason"`
}

type NotificationInput struct {
	HookInput
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
	Details          string `json:"details"`
}

var ToolNameMapping = map[string]events.ActionType{
	"read_file":         events.ActionFileRead,
	"list_directory":    events.ActionFileRead,
	"write_file":        events.ActionFileWrite,
	"run_shell_command": events.ActionCommandExec,
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
	case "BeforeTool":
		return a.parseBeforeTool(sessionID, agentSessionID, baseInput, rawData)
	case "AfterTool":
		return a.parseAfterTool(sessionID, agentSessionID, baseInput, rawData)
	case "SessionStart":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "SessionEnd":
		return parseSessionEnd(sessionID, agentSessionID, baseInput, rawData)
	case "Notification":
		return parseNotification(sessionID, agentSessionID, baseInput, rawData)
	default:
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.RawEvent = rawData
		return event, nil
	}
}

func resolveSessionID(rawSessionID string) uuid.UUID {
	if envID := os.Getenv("GEMINI_SESSION_ID"); envID != "" {
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

func (a *Adapter) parseBeforeTool(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input BeforeToolInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse BeforeTool input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func (a *Adapter) parseAfterTool(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input AfterToolInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse AfterTool input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, input.ToolResponse); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	event.ResultStatus = events.ResultSuccess
	detectErrorsInResponse(event, input.ToolResponse)

	a.markSensitivePaths(event, actionType, input.ToolInput)

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

	payload := events.SessionPayload{
		Source: input.Source,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

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

	payload := events.SessionEndPayload{
		Reason: input.Reason,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

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
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
		} else if path, ok := toolInput["path"].(string); ok {
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
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
			filePath = path
		}

		fullContent, _ := toolInput["content"].(string)
		fullOldStr, _ := toolInput["old_string"].(string)
		fullNewStr, _ := toolInput["new_string"].(string)

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
			return fmt.Errorf("failed to set payload: %w", err)
		}

		if a.loggingLevel.IsAtLeast(config.LoggingFull) {
			if fullOldStr != "" || fullNewStr != "" {
				event.DiffContent = utils.GenerateDiff(filePath, fullOldStr, fullNewStr)
			} else if fullContent != "" {
				event.DiffContent = utils.GenerateDiff(filePath, "", fullContent)
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
			if output, ok := toolResponse["output"].(string); ok {
				payload.Output = truncateString(output, 500)
			}
			if exitCode, ok := toolResponse["exitCode"].(float64); ok {
				payload.ExitCode = int(exitCode)
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

func detectErrorsInResponse(event *events.Event, response map[string]interface{}) {
	if response == nil {
		return
	}

	if errMsg, ok := response["error"].(string); ok && errMsg != "" {
		event.ResultStatus = events.ResultError
		event.ErrorMessage = truncateString(errMsg, 500)
		return
	}

	if success, ok := response["success"].(bool); ok && !success {
		event.ResultStatus = events.ResultError
		return
	}

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
