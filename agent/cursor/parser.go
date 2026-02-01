package cursor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

// HookInput represents the common fields in all Cursor hook inputs.
type HookInput struct {
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	Model          string   `json:"model,omitempty"`
	HookEventName  string   `json:"hook_event_name"`
	CursorVersion  string   `json:"cursor_version,omitempty"`
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
	UserEmail      string   `json:"user_email,omitempty"`
	TranscriptPath string   `json:"transcript_path,omitempty"`
}

// PreToolUseInput represents the input for preToolUse hooks.
type PreToolUseInput struct {
	HookInput
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolUseID    string                 `json:"tool_use_id"`
	Cwd          string                 `json:"cwd"`
	AgentMessage string                 `json:"agent_message,omitempty"`
}

// PostToolUseInput represents the input for postToolUse hooks.
type PostToolUseInput struct {
	HookInput
	ToolName   string                 `json:"tool_name"`
	ToolInput  map[string]interface{} `json:"tool_input"`
	ToolOutput string                 `json:"tool_output"`
	ToolUseID  string                 `json:"tool_use_id"`
	Cwd        string                 `json:"cwd"`
	Duration   int64                  `json:"duration"`
}

// PostToolUseFailureInput represents the input for postToolUseFailure hooks.
type PostToolUseFailureInput struct {
	HookInput
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolUseID    string                 `json:"tool_use_id"`
	Cwd          string                 `json:"cwd"`
	ErrorMessage string                 `json:"error_message"`
	FailureType  string                 `json:"failure_type"` // timeout, error, permission_denied
	Duration     int64                  `json:"duration"`
	IsInterrupt  bool                   `json:"is_interrupt"`
}

// BeforeShellExecutionInput represents the input for beforeShellExecution hooks.
type BeforeShellExecutionInput struct {
	HookInput
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Timeout int    `json:"timeout"`
}

// BeforeReadFileInput represents the input for beforeReadFile hooks.
type BeforeReadFileInput struct {
	HookInput
	FilePath string `json:"file_path"`
	Content  string `json:"content,omitempty"`
}

// AfterFileEditInput represents the input for afterFileEdit hooks.
type AfterFileEditInput struct {
	HookInput
	FilePath string `json:"file_path"`
	Edits    []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// BeforeSubmitPromptInput represents the input for beforeSubmitPrompt hooks.
type BeforeSubmitPromptInput struct {
	HookInput
	Prompt string `json:"prompt"`
}

// SessionStartInput represents the input for sessionStart hooks.
type SessionStartInput struct {
	HookInput
	SessionID         string `json:"session_id"`
	IsBackgroundAgent bool   `json:"is_background_agent"`
	ComposerMode      string `json:"composer_mode"` // agent, ask, edit
}

// SessionEndInput represents the input for sessionEnd hooks.
type SessionEndInput struct {
	HookInput
	SessionID         string `json:"session_id"`
	Reason            string `json:"reason"` // completed, aborted, error, window_close, user_close
	DurationMs        int64  `json:"duration_ms"`
	IsBackgroundAgent bool   `json:"is_background_agent"`
	FinalStatus       string `json:"final_status,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
}

// StopInput represents the input for stop hooks.
type StopInput struct {
	HookInput
	Status    string `json:"status"` // completed, aborted, error
	LoopCount int    `json:"loop_count"`
}

// BeforeMCPExecutionInput represents the input for beforeMCPExecution hooks.
type BeforeMCPExecutionInput struct {
	HookInput
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	URL       string                 `json:"url,omitempty"`
	Command   string                 `json:"command,omitempty"`
	Cwd       string                 `json:"cwd,omitempty"`
}

// AfterShellExecutionInput represents the input for afterShellExecution hooks.
type AfterShellExecutionInput struct {
	HookInput
	Command  string `json:"command"`
	Output   string `json:"output"`
	Duration int64  `json:"duration"`
	Cwd      string `json:"cwd,omitempty"`
}

// AfterMCPExecutionInput represents the input for afterMCPExecution hooks.
type AfterMCPExecutionInput struct {
	HookInput
	ToolName   string                 `json:"tool_name"`
	ToolInput  map[string]interface{} `json:"tool_input"`
	ResultJSON map[string]interface{} `json:"result_json,omitempty"`
	Duration   int64                  `json:"duration"`
}

// SubagentStartInput represents the input for subagentStart hooks.
type SubagentStartInput struct {
	HookInput
	SubagentType string `json:"subagent_type"`
	Prompt       string `json:"prompt,omitempty"`
	SubModel     string `json:"model,omitempty"`
}

// SubagentStopInput represents the input for subagentStop hooks.
type SubagentStopInput struct {
	HookInput
	SubagentType string `json:"subagent_type"`
	Status       string `json:"status"`
	Result       string `json:"result,omitempty"`
	Duration     int64  `json:"duration"`
}

// AfterAgentThoughtInput represents the input for afterAgentThought hooks.
type AfterAgentThoughtInput struct {
	HookInput
	Text       string `json:"text"`
	DurationMs int64  `json:"duration_ms"`
}

// HookTypeMapping maps Cursor hook types to action types.
var HookTypeMapping = map[string]events.ActionType{
	"preToolUse":           events.ActionToolUse,
	"postToolUse":          events.ActionToolUse,
	"postToolUseFailure":   events.ActionToolUse,
	"beforeShellExecution": events.ActionCommandExec,
	"afterShellExecution":  events.ActionCommandExec,
	"beforeMCPExecution":   events.ActionToolUse,
	"afterMCPExecution":    events.ActionToolUse,
	"beforeReadFile":       events.ActionFileRead,
	"afterFileEdit":        events.ActionFileWrite,
	"beforeTabFileRead":    events.ActionFileRead,
	"afterTabFileEdit":     events.ActionFileWrite,
	"beforeSubmitPrompt":   events.ActionToolUse,
	"afterAgentResponse":   events.ActionToolUse,
	"subagentStart":        events.ActionToolUse,
	"subagentStop":         events.ActionToolUse,
	"afterAgentThought":    events.ActionToolUse,
	"sessionStart":         events.ActionSessionStart,
	"sessionEnd":           events.ActionSessionEnd,
	"stop":                 events.ActionSessionEnd,
	"preCompact":           events.ActionToolUse,
}

// ToolNameToActionType maps Cursor tool names to action types.
var ToolNameToActionType = map[string]events.ActionType{
	"Shell": events.ActionCommandExec,
	"Read":  events.ActionFileRead,
	"Write": events.ActionFileWrite,
	"Edit":  events.ActionFileWrite,
	"Grep":  events.ActionFileRead,
	"Glob":  events.ActionFileRead,
	"Task":  events.ActionToolUse,
}

func (a *Adapter) parseHookEvent(hookType string, rawData []byte) (*events.Event, error) {
	var baseInput HookInput
	if err := json.Unmarshal(rawData, &baseInput); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}

	var sessionID uuid.UUID
	if baseInput.ConversationID != "" {
		var err error
		sessionID, err = uuid.Parse(baseInput.ConversationID)
		if err != nil {
			sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(baseInput.ConversationID))
		}
	} else {
		sessionID = uuid.New()
	}

	agentSessionID := baseInput.ConversationID

	switch hookType {
	case "preToolUse":
		return a.parsePreToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "postToolUse":
		return a.parsePostToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "postToolUseFailure":
		return a.parsePostToolUseFailure(sessionID, agentSessionID, baseInput, rawData)
	case "beforeShellExecution":
		return parseBeforeShellExecution(sessionID, agentSessionID, baseInput, rawData)
	case "beforeReadFile":
		return a.parseBeforeReadFile(sessionID, agentSessionID, baseInput, rawData)
	case "afterFileEdit":
		return a.parseAfterFileEdit(sessionID, agentSessionID, baseInput, rawData)
	case "beforeSubmitPrompt":
		return parseBeforeSubmitPrompt(sessionID, agentSessionID, baseInput, rawData)
	case "sessionStart":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "sessionEnd":
		return parseSessionEnd(sessionID, agentSessionID, baseInput, rawData)
	case "stop":
		return parseStop(sessionID, agentSessionID, baseInput, rawData)
	case "beforeTabFileRead":
		return a.parseBeforeReadFile(sessionID, agentSessionID, baseInput, rawData)
	case "afterTabFileEdit":
		return a.parseAfterFileEdit(sessionID, agentSessionID, baseInput, rawData)
	case "beforeMCPExecution":
		return parseBeforeMCPExecution(sessionID, agentSessionID, baseInput, rawData)
	case "afterShellExecution":
		return parseAfterShellExecution(sessionID, agentSessionID, baseInput, rawData)
	case "afterMCPExecution":
		return parseAfterMCPExecution(sessionID, agentSessionID, baseInput, rawData)
	case "subagentStart":
		return parseSubagentStart(sessionID, agentSessionID, baseInput, rawData)
	case "subagentStop":
		return parseSubagentStop(sessionID, agentSessionID, baseInput, rawData)
	case "afterAgentThought":
		return parseAfterAgentThought(sessionID, agentSessionID, baseInput, rawData)
	default:
		actionType := events.ActionUnknown
		if at, ok := HookTypeMapping[hookType]; ok {
			actionType = at
		}
		event := events.NewEvent(sessionID, AgentName, actionType)
		event.AgentSessionID = agentSessionID
		event.ToolName = hookType
		event.RawEvent = rawData
		if len(baseInput.WorkspaceRoots) > 0 {
			event.WorkingDirectory = baseInput.WorkspaceRoots[0]
		}
		return event, nil
	}
}

func (a *Adapter) parsePreToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PreToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse preToolUse input: %w", err)
	}

	actionType := events.ActionToolUse
	if at, ok := ToolNameToActionType[input.ToolName]; ok {
		actionType = at
	}

	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) parsePostToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PostToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse postToolUse input: %w", err)
	}

	actionType := events.ActionToolUse
	if at, ok := ToolNameToActionType[input.ToolName]; ok {
		actionType = at
	}

	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.DurationMs = input.Duration
	event.RawEvent = rawData
	event.ResultStatus = events.ResultSuccess

	toolOutput := map[string]interface{}{"output": input.ToolOutput}
	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, toolOutput); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) parsePostToolUseFailure(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PostToolUseFailureInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse postToolUseFailure input: %w", err)
	}

	actionType := events.ActionToolUse
	if at, ok := ToolNameToActionType[input.ToolName]; ok {
		actionType = at
	}

	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.DurationMs = input.Duration
	event.RawEvent = rawData
	event.ResultStatus = events.ResultError
	event.ErrorMessage = input.ErrorMessage

	if err := a.buildPayload(event, actionType, input.ToolName, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	return event, nil
}

func parseBeforeShellExecution(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input BeforeShellExecutionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse beforeShellExecution input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionCommandExec)
	event.AgentSessionID = agentSessionID
	event.ToolName = "Shell"
	event.WorkingDirectory = input.Cwd
	event.RawEvent = rawData

	payload := events.CommandExecPayload{
		Command: input.Command,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) parseBeforeReadFile(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input BeforeReadFileInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse beforeReadFile input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionFileRead)
	event.AgentSessionID = agentSessionID
	event.ToolName = "Read"
	event.RawEvent = rawData

	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.FileReadPayload{
		Path: input.FilePath,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	a.markSensitivePath(event, input.FilePath)

	return event, nil
}

func (a *Adapter) parseAfterFileEdit(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input AfterFileEditInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse afterFileEdit input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionFileWrite)
	event.AgentSessionID = agentSessionID
	event.ToolName = "Edit"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.FileWritePayload{
		Path: input.FilePath,
	}
	if len(input.Edits) > 0 {
		payload.OldString = truncateString(input.Edits[0].OldString, 200)
		payload.NewString = truncateString(input.Edits[0].NewString, 200)
	}

	if a.contentHash && len(input.Edits) > 0 {
		var combined string
		for _, edit := range input.Edits {
			combined += edit.OldString + edit.NewString
		}
		payload.ContentHash = utils.HashContent(combined)
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	if a.loggingLevel.IsAtLeast(config.LoggingFull) && len(input.Edits) > 0 {
		var diffBuilder strings.Builder
		for _, edit := range input.Edits {
			diffBuilder.WriteString(utils.GenerateDiff(input.FilePath, edit.OldString, edit.NewString))
		}
		event.DiffContent = diffBuilder.String()
	}

	a.markSensitivePath(event, input.FilePath)

	return event, nil
}

func parseBeforeSubmitPrompt(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input BeforeSubmitPromptInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse beforeSubmitPrompt input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "beforeSubmitPrompt"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: "beforeSubmitPrompt",
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSessionStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionStartInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse sessionStart input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionStart)
	event.AgentSessionID = agentSessionID
	event.ToolName = "sessionStart"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.SessionPayload{
		Model: base.Model,
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSessionEnd(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionEndInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse sessionEnd input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.ToolName = "sessionEnd"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.SessionEndPayload{
		Reason: input.Reason,
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseStop(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input StopInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse stop input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.ToolName = "stop"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.SessionEndPayload{
		Reason: input.Status,
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseBeforeMCPExecution(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input BeforeMCPExecutionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse beforeMCPExecution input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.RawEvent = rawData
	if input.Cwd != "" {
		event.WorkingDirectory = input.Cwd
	} else if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: input.ToolName,
	}
	if inputBytes, err := json.Marshal(input.ToolInput); err == nil {
		payload.Input = inputBytes
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseAfterShellExecution(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input AfterShellExecutionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse afterShellExecution input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionCommandExec)
	event.AgentSessionID = agentSessionID
	event.ToolName = "Shell"
	event.DurationMs = input.Duration
	event.RawEvent = rawData
	event.ResultStatus = events.ResultSuccess
	if input.Cwd != "" {
		event.WorkingDirectory = input.Cwd
	} else if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.CommandExecPayload{
		Command: input.Command,
		Output:  truncateString(input.Output, 500),
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseAfterMCPExecution(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input AfterMCPExecutionInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse afterMCPExecution input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.DurationMs = input.Duration
	event.RawEvent = rawData
	event.ResultStatus = events.ResultSuccess
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: input.ToolName,
	}
	if inputBytes, err := json.Marshal(input.ToolInput); err == nil {
		payload.Input = inputBytes
	}
	if input.ResultJSON != nil {
		if resultBytes, err := json.Marshal(input.ResultJSON); err == nil {
			payload.Output = resultBytes
		}
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSubagentStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SubagentStartInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse subagentStart input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "subagentStart"
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: "subagentStart",
	}
	inputMap := map[string]interface{}{
		"subagent_type": input.SubagentType,
		"prompt":        input.Prompt,
		"model":         input.SubModel,
	}
	if inputBytes, err := json.Marshal(inputMap); err == nil {
		payload.Input = inputBytes
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSubagentStop(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SubagentStopInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse subagentStop input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "subagentStop"
	event.DurationMs = input.Duration
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: "subagentStop",
	}
	inputMap := map[string]interface{}{
		"subagent_type": input.SubagentType,
		"status":        input.Status,
		"result":        input.Result,
	}
	if inputBytes, err := json.Marshal(inputMap); err == nil {
		payload.Input = inputBytes
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseAfterAgentThought(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input AfterAgentThoughtInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse afterAgentThought input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "afterAgentThought"
	event.DurationMs = input.DurationMs
	event.RawEvent = rawData
	if len(base.WorkspaceRoots) > 0 {
		event.WorkingDirectory = base.WorkspaceRoots[0]
	}

	payload := events.ToolUsePayload{
		ToolName: "afterAgentThought",
	}
	inputMap := map[string]interface{}{
		"text": input.Text,
	}
	if inputBytes, err := json.Marshal(inputMap); err == nil {
		payload.Input = inputBytes
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) buildPayload(event *events.Event, actionType events.ActionType, toolName string, toolInput, toolOutput map[string]interface{}) error {
	switch actionType {
	case events.ActionFileRead:
		payload := events.FileReadPayload{}
		if path, ok := toolInput["file_path"].(string); ok {
			payload.Path = path
			a.markSensitivePath(event, path)
		} else if path, ok := toolInput["path"].(string); ok {
			payload.Path = path
			a.markSensitivePath(event, path)
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
			a.markSensitivePath(event, path)
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
		if toolOutput != nil {
			if output, ok := toolOutput["output"].(string); ok {
				payload.Output = truncateString(output, 500)
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
		if toolOutput != nil {
			if resp, err := json.Marshal(toolOutput); err == nil {
				payload.Output = resp
			}
		}
		if err := event.SetPayload(payload); err != nil {
			return fmt.Errorf("failed to set payload: %w", err)
		}
	}

	return nil
}

func (a *Adapter) markSensitivePath(event *events.Event, path string) {
	if path == "" || a.privacyChecker == nil {
		return
	}
	event.IsSensitive = a.privacyChecker.IsSensitivePath(path)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// HookDecision represents the decision for a Cursor hook.
type HookDecision int

const (
	// HookAllow allows the action to proceed.
	HookAllow HookDecision = iota
	// HookDeny blocks the action.
	HookDeny
	// HookAsk prompts the user to confirm (only for some hooks).
	HookAsk
)

// HookResponse represents a response to Cursor hooks.
type HookResponse struct {
	// Decision is whether to allow, deny, or ask.
	Decision HookDecision
	// Reason is the reason for the decision (shown to agent or user).
	Reason string
}

// NewAllowResponse creates a response that allows the action.
func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

// NewDenyResponse creates a response that denies the action.
func NewDenyResponse(reason string) *HookResponse {
	return &HookResponse{
		Decision: HookDeny,
		Reason:   reason,
	}
}

// NewAskResponse creates a response that asks the user to confirm.
func NewAskResponse(reason string) *HookResponse {
	return &HookResponse{
		Decision: HookAsk,
		Reason:   reason,
	}
}

// GeneratePreToolUseResponse generates a response for preToolUse hooks.
func GeneratePreToolUseResponse(response *HookResponse) []byte {
	output := map[string]interface{}{}
	switch response.Decision {
	case HookAllow:
		output["decision"] = "allow"
	case HookDeny:
		output["decision"] = "deny"
		if response.Reason != "" {
			output["reason"] = response.Reason
		}
	}
	data, _ := json.Marshal(output)
	return data
}

// GeneratePermissionResponse generates a response for beforeShellExecution, beforeMCPExecution, beforeReadFile hooks.
func GeneratePermissionResponse(response *HookResponse) []byte {
	output := map[string]interface{}{}
	switch response.Decision {
	case HookAllow:
		output["permission"] = "allow"
	case HookDeny:
		output["permission"] = "deny"
		if response.Reason != "" {
			output["user_message"] = response.Reason
		}
	case HookAsk:
		output["permission"] = "ask"
		if response.Reason != "" {
			output["user_message"] = response.Reason
		}
	}
	data, _ := json.Marshal(output)
	return data
}

// GenerateContinueResponse generates a response for beforeSubmitPrompt and sessionStart hooks.
func GenerateContinueResponse(cont bool, message string) []byte {
	output := map[string]interface{}{
		"continue": cont,
	}
	if message != "" {
		output["user_message"] = message
	}
	data, _ := json.Marshal(output)
	return data
}

// GenerateStopResponse generates a response for stop and subagentStop hooks.
func GenerateStopResponse(followupMessage string) []byte {
	output := map[string]interface{}{}
	if followupMessage != "" {
		output["followup_message"] = followupMessage
	}
	data, _ := json.Marshal(output)
	return data
}

// GenerateResponse generates a generic allow/deny response (for backwards compatibility).
// Deprecated: Use the hook-specific response generators instead.
func GenerateResponse(allow bool, message string) []byte {
	response := &HookResponse{Decision: HookAllow}
	if !allow {
		response = &HookResponse{Decision: HookDeny, Reason: message}
	}
	return GeneratePreToolUseResponse(response)
}
