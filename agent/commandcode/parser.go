package commandcode

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

// HookInput represents the common fields in all Command Code hook inputs.
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

// PostToolUseInput represents the input for PostToolUse hooks.
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
	Source string `json:"source"`
}

// StopInput represents the input for Stop hooks. Stop fires when the
// assistant finishes a turn and carries no tool fields.
type StopInput struct {
	HookInput
	StopHookActive bool `json:"stop_hook_active"`
}

// ToolNameMapping maps Command Code tool names to action types.
var ToolNameMapping = map[string]events.ActionType{
	"shell_command": events.ActionCommandExec,
	"read_file":     events.ActionFileRead,
	"write_file":    events.ActionFileWrite,
	"edit_file":     events.ActionFileWrite,
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

	var sessionID uuid.UUID
	if baseInput.SessionID != "" {
		var err error
		sessionID, err = uuid.Parse(baseInput.SessionID)
		if err != nil {
			sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(baseInput.SessionID))
		}
	} else {
		sessionID = uuid.New()
	}

	agentSessionID := baseInput.SessionID

	var event *events.Event
	var parseErr error

	switch eventName {
	case "PreToolUse":
		event, parseErr = a.parsePreToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "PostToolUse":
		event, parseErr = a.parsePostToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "Stop":
		event, parseErr = parseStop(sessionID, agentSessionID, baseInput, rawData)
	case "SessionStart":
		event, parseErr = parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	default:
		event = events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.RawEvent = rawData
	}

	if parseErr != nil {
		return nil, parseErr
	}
	if event != nil {
		event.TranscriptPath = baseInput.TranscriptPath
		event.HookType = eventName
	}
	return event, nil
}

func (a *Adapter) parsePreToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
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

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func (a *Adapter) parsePostToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	origRawData := rawData

	var input PostToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		rawData, err = wrapToolResponse(rawData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PostToolUse input: %w", err)
		}
		if err := json.Unmarshal(rawData, &input); err != nil {
			return nil, fmt.Errorf("failed to parse PostToolUse input after wrapping tool_response: %w", err)
		}
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = origRawData

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, input.ToolResponse); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	event.ResultStatus = events.ResultSuccess
	detectErrorsInResponse(event, input.ToolResponse)

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

// wrapToolResponse rewrites a non-object tool_response value (Command Code
// sends the tool output as a plain string) into {"output": <original_value>}
// so it can be unmarshalled into PostToolUseInput.ToolResponse as
// map[string]interface{}.
func wrapToolResponse(rawData []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rawData, &raw); err != nil {
		return nil, err
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{"output": raw["tool_response"]})
	if err != nil {
		return nil, err
	}
	raw["tool_response"] = wrapped
	return json.Marshal(raw)
}

func parseStop(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input StopInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse Stop input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	payload := events.SessionEndPayload{
		Reason: "turn_end",
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

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
		if path, ok := readToolPath(toolInput); ok {
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

		// edit_file carries the change as old_value/new_value; accept the
		// old_string/new_string spellings too for robustness.
		fullOldStr, _ := firstString(toolInput, "old_value", "old_string")
		fullNewStr, _ := firstString(toolInput, "new_value", "new_string")
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
			event.FullContent = fullContent
		}
		if fullOldStr != "" {
			payload.OldString = truncateString(fullOldStr, 200)
		}
		if fullNewStr != "" {
			payload.NewString = truncateString(fullNewStr, 200)
		}
		// Edit writes carry their content in new_value; feed it to the matcher.
		if event.FullContent == "" && fullNewStr != "" {
			event.FullContent = fullNewStr
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
		// shell_command splits argv into an optional args array.
		if args, ok := toolInput["args"].([]interface{}); ok && len(args) > 0 {
			var strArgs []string
			for _, arg := range args {
				if s, ok := arg.(string); ok {
					strArgs = append(strArgs, s)
				}
			}
			if len(strArgs) > 0 {
				payload.Command = strings.TrimSpace(payload.Command + " " + strings.Join(strArgs, " "))
			}
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

// readToolPath extracts the target path of a read. Command Code's read_file
// uses absolute_path; the other spellings are accepted for robustness.
func readToolPath(toolInput map[string]interface{}) (string, bool) {
	path, ok := firstString(toolInput, "absolute_path", "file_path", "path")
	return path, ok
}

// firstString returns the first key present in toolInput with a non-empty
// string value.
func firstString(toolInput map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if v, ok := toolInput[key].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
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

func (a *Adapter) markSensitivePaths(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}) {
	if a.privacyChecker == nil {
		return
	}

	switch actionType {
	case events.ActionFileRead, events.ActionFileWrite:
		if path, ok := readToolPath(toolInput); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(path)
		} else if path, ok := toolInput["file_path"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(path)
		}
	case events.ActionCommandExec:
		if cmd, ok := toolInput["command"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(cmd)
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

// HookDecision represents the decision for a Command Code hook.
type HookDecision int

const (
	// HookAllow allows the action to proceed (exit code 0).
	HookAllow HookDecision = iota
	// HookBlock blocks the action (exit code 2, message to stderr, shown to the model).
	HookBlock
	// HookError is a non-blocking error (exit code 1, message to stderr, shown to user in verbose mode).
	HookError
	// HookGuidance allows the action but emits advisory text to stderr (exit code 0, stderr shown to user in verbose mode).
	// Used when a security check returns DecisionGuidance — advisory, not blocking.
	HookGuidance
)

// HookResponse represents a response to Command Code hooks.
type HookResponse struct {
	// Decision is whether to allow, block, or report error.
	Decision HookDecision
	// Message is the reason (used for HookBlock and HookError).
	Message string
}

// ExitCode returns the exit code for this response.
// Exit code 0 = allow (or guidance), exit code 2 = block, exit code 1 = non-blocking error.
func (r *HookResponse) ExitCode() int {
	switch r.Decision {
	case HookBlock:
		return 2
	case HookError:
		return 1
	default:
		// HookAllow and HookGuidance both exit 0 (guidance is non-blocking).
		return 0
	}
}

// Stderr returns the message to write to stderr.
// Written for HookBlock (shown to the model), HookError (verbose mode), and HookGuidance (advisory, verbose mode).
func (r *HookResponse) Stderr() string {
	switch r.Decision {
	case HookBlock, HookError, HookGuidance:
		return r.Message
	default:
		return ""
	}
}

// NewAllowResponse creates a response that allows the action.
func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

// NewBlockResponse creates a response that blocks the action with a reason.
// The message is shown to the model.
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

// NewGuidanceResponse creates a non-blocking advisory response.
// Exit code 0 (allow); message shown to user in verbose mode. Used when
// a security check returns DecisionGuidance — advisory, not blocking.
func NewGuidanceResponse(message string) *HookResponse {
	return &HookResponse{
		Decision: HookGuidance,
		Message:  message,
	}
}
