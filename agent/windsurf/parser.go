package windsurf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

type HookInput struct {
	AgentActionName string          `json:"agent_action_name"`
	TrajectoryID    string          `json:"trajectory_id"`
	ExecutionID     string          `json:"execution_id"`
	Timestamp       string          `json:"timestamp"`
	ToolInfo        json.RawMessage `json:"tool_info"`
}

type CodeToolInfo struct {
	FilePath string `json:"file_path"`
	Edits    []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits,omitempty"`
}

type CommandToolInfo struct {
	CommandLine string `json:"command_line"`
	Cwd         string `json:"cwd"`
}

type MCPToolInfo struct {
	MCPServerName    string                 `json:"mcp_server_name"`
	MCPToolName      string                 `json:"mcp_tool_name"`
	MCPToolArguments map[string]interface{} `json:"mcp_tool_arguments"`
	MCPResult        string                 `json:"mcp_result,omitempty"`
}

type PromptToolInfo struct {
	UserPrompt string `json:"user_prompt,omitempty"`
	Response   string `json:"response,omitempty"`
}

type WorktreeToolInfo struct {
	WorktreePath      string `json:"worktree_path"`
	RootWorkspacePath string `json:"root_workspace_path"`
}

var HookTypeMapping = map[string]events.ActionType{
	"pre_read_code":         events.ActionFileRead,
	"post_read_code":        events.ActionFileRead,
	"pre_write_code":        events.ActionFileWrite,
	"post_write_code":       events.ActionFileWrite,
	"pre_run_command":       events.ActionCommandExec,
	"post_run_command":      events.ActionCommandExec,
	"pre_mcp_tool_use":      events.ActionToolUse,
	"post_mcp_tool_use":     events.ActionToolUse,
	"pre_user_prompt":       events.ActionToolUse,
	"post_cascade_response": events.ActionNotification,
	"post_setup_worktree":   events.ActionToolUse,
}

func isPreHook(hookType string) bool {
	return strings.HasPrefix(hookType, "pre_")
}

func (a *Adapter) parseHookEvent(hookType string, rawData []byte) (*events.Event, error) {
	var input HookInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}

	var sessionID uuid.UUID
	if input.TrajectoryID != "" {
		sessionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(input.TrajectoryID))
	} else {
		sessionID = uuid.New()
	}

	agentSessionID := input.TrajectoryID

	switch hookType {
	case "pre_read_code":
		return a.parseReadCode(sessionID, agentSessionID, input, false)
	case "post_read_code":
		return a.parseReadCode(sessionID, agentSessionID, input, true)
	case "pre_write_code":
		return a.parseWriteCode(sessionID, agentSessionID, input, false)
	case "post_write_code":
		return a.parseWriteCode(sessionID, agentSessionID, input, true)
	case "pre_run_command":
		return parseRunCommand(sessionID, agentSessionID, input, false)
	case "post_run_command":
		return parseRunCommand(sessionID, agentSessionID, input, true)
	case "pre_mcp_tool_use":
		return parseMCPToolUse(sessionID, agentSessionID, input, false)
	case "post_mcp_tool_use":
		return parseMCPToolUse(sessionID, agentSessionID, input, true)
	case "pre_user_prompt":
		return parseUserPrompt(sessionID, agentSessionID, input)
	case "post_cascade_response":
		return parseCascadeResponse(sessionID, agentSessionID, input)
	case "post_setup_worktree":
		return parseSetupWorktree(sessionID, agentSessionID, input)
	default:
		actionType := events.ActionUnknown
		if at, ok := HookTypeMapping[hookType]; ok {
			actionType = at
		}
		event := events.NewEvent(sessionID, AgentName, actionType)
		event.AgentSessionID = agentSessionID
		event.ToolName = hookType
		event.RawEvent = rawData
		return event, nil
	}
}

func (a *Adapter) parseReadCode(sessionID uuid.UUID, agentSessionID string, input HookInput, isPost bool) (*events.Event, error) {
	var info CodeToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse read_code tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionFileRead)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.AgentActionName
	event.RawEvent = nil

	if isPost {
		event.ResultStatus = events.ResultSuccess
	}

	payload := events.FileReadPayload{
		Path: info.FilePath,
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	a.markSensitivePath(event, info.FilePath)

	return event, nil
}

func (a *Adapter) parseWriteCode(sessionID uuid.UUID, agentSessionID string, input HookInput, isPost bool) (*events.Event, error) {
	var info CodeToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse write_code tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionFileWrite)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.AgentActionName
	event.RawEvent = nil

	if isPost {
		event.ResultStatus = events.ResultSuccess
	}

	payload := events.FileWritePayload{
		Path: info.FilePath,
	}

	if len(info.Edits) > 0 {
		for _, edit := range info.Edits {
			added, removed := utils.CountDiffLines(edit.OldString, edit.NewString)
			payload.LinesAdded += added
			payload.LinesRemoved += removed
		}
		payload.OldString = truncateString(info.Edits[0].OldString, 200)
		payload.NewString = truncateString(info.Edits[0].NewString, 200)
	}

	if a.contentHash && len(info.Edits) > 0 {
		var combined string
		for _, edit := range info.Edits {
			combined += edit.OldString + edit.NewString
		}
		payload.ContentHash = utils.HashContent(combined)
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	if a.loggingLevel.IsAtLeast(config.LoggingFull) && len(info.Edits) > 0 {
		var diffBuilder strings.Builder
		for _, edit := range info.Edits {
			diffBuilder.WriteString(utils.GenerateDiff(info.FilePath, edit.OldString, edit.NewString))
		}
		event.DiffContent = diffBuilder.String()
	}

	a.markSensitivePath(event, info.FilePath)

	return event, nil
}

func parseRunCommand(sessionID uuid.UUID, agentSessionID string, input HookInput, isPost bool) (*events.Event, error) {
	var info CommandToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse run_command tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionCommandExec)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.AgentActionName
	event.WorkingDirectory = info.Cwd
	event.RawEvent = nil

	if isPost {
		event.ResultStatus = events.ResultSuccess
	}

	payload := events.CommandExecPayload{
		Command: info.CommandLine,
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseMCPToolUse(sessionID uuid.UUID, agentSessionID string, input HookInput, isPost bool) (*events.Event, error) {
	var info MCPToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse mcp_tool_use tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = info.MCPToolName
	event.RawEvent = nil

	if isPost {
		event.ResultStatus = events.ResultSuccess
	}

	payload := events.ToolUsePayload{
		ToolName: fmt.Sprintf("%s/%s", info.MCPServerName, info.MCPToolName),
	}
	if info.MCPToolArguments != nil {
		if inputBytes, err := json.Marshal(info.MCPToolArguments); err == nil {
			payload.Input = inputBytes
		}
	}
	if isPost && info.MCPResult != "" {
		if resultBytes, err := json.Marshal(map[string]string{"result": info.MCPResult}); err == nil {
			payload.Output = resultBytes
		}
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseUserPrompt(sessionID uuid.UUID, agentSessionID string, input HookInput) (*events.Event, error) {
	var info PromptToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse user_prompt tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "pre_user_prompt"
	event.RawEvent = nil

	payload := events.ToolUsePayload{
		ToolName: "pre_user_prompt",
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseCascadeResponse(sessionID uuid.UUID, agentSessionID string, input HookInput) (*events.Event, error) {
	var info PromptToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse cascade_response tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionNotification)
	event.AgentSessionID = agentSessionID
	event.ToolName = "post_cascade_response"
	event.ResultStatus = events.ResultSuccess
	event.RawEvent = nil

	payload := events.ToolUsePayload{
		ToolName: "post_cascade_response",
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseSetupWorktree(sessionID uuid.UUID, agentSessionID string, input HookInput) (*events.Event, error) {
	var info WorktreeToolInfo
	if err := json.Unmarshal(input.ToolInfo, &info); err != nil {
		return nil, fmt.Errorf("failed to parse setup_worktree tool_info: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "post_setup_worktree"
	event.WorkingDirectory = info.WorktreePath
	event.ResultStatus = events.ResultSuccess
	event.RawEvent = nil

	payload := events.ToolUsePayload{
		ToolName: "post_setup_worktree",
	}
	inputMap := map[string]interface{}{
		"worktree_path":       info.WorktreePath,
		"root_workspace_path": info.RootWorkspacePath,
	}
	if inputBytes, err := json.Marshal(inputMap); err == nil {
		payload.Input = inputBytes
	}
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
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

func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

func NewBlockResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookBlock, Message: message}
}

func NewErrorResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookError, Message: message}
}
