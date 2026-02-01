package opencode

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

type ToolEventInput struct {
	HookType string                 `json:"hook_type"`
	Tool     string                 `json:"tool"`
	Args     map[string]interface{} `json:"args"`
	Cwd      string                 `json:"cwd"`
}

type SessionEventInput struct {
	HookType   string                 `json:"hook_type"`
	Properties map[string]interface{} `json:"properties"`
	Cwd        string                 `json:"cwd"`
}

var ToolNameMapping = map[string]events.ActionType{
	"read":      events.ActionFileRead,
	"grep":      events.ActionFileRead,
	"glob":      events.ActionFileRead,
	"list":      events.ActionFileRead,
	"write":     events.ActionFileWrite,
	"edit":      events.ActionFileWrite,
	"patch":     events.ActionFileWrite,
	"bash":      events.ActionCommandExec,
	"webfetch":  events.ActionToolUse,
	"question":  events.ActionToolUse,
	"todowrite": events.ActionToolUse,
	"todoread":  events.ActionToolUse,
	"lsp":       events.ActionToolUse,
	"skill":     events.ActionToolUse,
}

func (a *Adapter) parseHookEvent(hookType string, rawData []byte) (*events.Event, error) {
	switch hookType {
	case "tool.execute.before":
		return a.parseToolEvent(hookType, rawData, false)
	case "tool.execute.after":
		return a.parseToolEvent(hookType, rawData, true)
	case "session.created":
		return a.parseSessionEvent(hookType, rawData, events.ActionSessionStart)
	case "session.idle":
		return a.parseSessionEvent(hookType, rawData, events.ActionSessionEnd)
	case "session.error":
		return a.parseSessionEvent(hookType, rawData, events.ActionNotification)
	default:
		sessionID := resolveSessionID("")
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.RawEvent = rawData
		return event, nil
	}
}

func (a *Adapter) parseToolEvent(hookType string, rawData []byte, isAfter bool) (*events.Event, error) {
	var input ToolEventInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse tool event input: %w", err)
	}

	sessionID := resolveSessionID("")
	toolName := strings.ToLower(input.Tool)
	actionType := getActionType(toolName)

	event := events.NewEvent(sessionID, AgentName, actionType)
	event.ToolName = toolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	if err := a.buildPayload(event, actionType, toolName, input.Args, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	if isAfter {
		event.ResultStatus = events.ResultSuccess
	}

	a.markSensitivePaths(event, actionType, input.Args)

	return event, nil
}

func (a *Adapter) parseSessionEvent(hookType string, rawData []byte, actionType events.ActionType) (*events.Event, error) {
	var input SessionEventInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse session event input: %w", err)
	}

	sessionIDStr, _ := input.Properties["sessionId"].(string)
	sessionID := resolveSessionID(sessionIDStr)
	agentSessionID := sessionIDStr

	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	switch actionType {
	case events.ActionSessionStart:
		payload := events.SessionPayload{
			Source: "opencode",
		}
		if err := event.SetPayload(payload); err != nil {
			return nil, fmt.Errorf("failed to set payload: %w", err)
		}

	case events.ActionSessionEnd:
		payload := events.SessionEndPayload{
			Reason: "idle",
		}
		if err := event.SetPayload(payload); err != nil {
			return nil, fmt.Errorf("failed to set payload: %w", err)
		}

	case events.ActionNotification:
		message, _ := input.Properties["message"].(string)
		payload := events.NotificationPayload{
			Message: message,
			Type:    "error",
		}
		if err := event.SetPayload(payload); err != nil {
			return nil, fmt.Errorf("failed to set payload: %w", err)
		}
	}

	return event, nil
}

func resolveSessionID(rawSessionID string) uuid.UUID {
	if envID := os.Getenv("OPENCODE_SESSION_ID"); envID != "" {
		rawSessionID = envID
	}

	if rawSessionID != "" {
		if parsed, err := uuid.Parse(rawSessionID); err == nil {
			return parsed
		}
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte(rawSessionID))
	}

	cwd, _ := os.Getwd()
	ppid := fmt.Sprintf("%d", os.Getppid())
	fallback := cwd + ":" + ppid
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fallback))
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
