package events

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/privacy"
)

// EventSchemaURL is the canonical URL for the Event JSON Schema hosted on GitHub.
const EventSchemaURL = "https://raw.githubusercontent.com/safedep/gryph/main/schema/event.schema.json"

// Event represents a single action performed by an agent.
type Event struct {
	// ID is the unique identifier for this event.
	ID uuid.UUID `json:"id"`
	// SessionID links this event to its parent session (deterministic UUID derived from AgentSessionID).
	SessionID uuid.UUID `json:"session_id"`
	// AgentSessionID is the original session ID string from the agent (for correlation).
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// Sequence is the order within the session (1, 2, 3...).
	Sequence int `json:"sequence"`
	// Timestamp is when the action occurred (UTC).
	Timestamp time.Time `json:"timestamp"`
	// DurationMs is how long the action took in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`
	// AgentName is the identifier of the agent (e.g., "claude-code").
	AgentName string `json:"agent_name"`
	// AgentVersion is the version of the agent if detectable.
	AgentVersion string `json:"agent_version,omitempty"`
	// WorkingDirectory may differ from session if agent changed dirs.
	WorkingDirectory string `json:"working_directory,omitempty"`
	// ActionType is the category of action performed.
	ActionType ActionType `json:"action_type"`
	// ToolName is the original tool name from the agent.
	ToolName string `json:"tool_name,omitempty"`
	// ResultStatus is the outcome of the action.
	ResultStatus ResultStatus `json:"result_status"`
	// ErrorMessage contains error details if status is error.
	ErrorMessage string `json:"error_message,omitempty"`
	// Payload contains action-specific data.
	Payload json.RawMessage `json:"payload,omitempty"`
	// DiffContent contains file diff (full logging only, never for sensitive paths).
	DiffContent privacy.Text `json:"diff_content,omitzero"`
	// RawEvent is the original event from agent (full logging only).
	RawEvent json.RawMessage `json:"raw_event,omitempty"`
	// TranscriptPath is the path to the agent's transcript file (if provided by the agent).
	// Excluded from JSON export as it is internal to the local machine.
	TranscriptPath string `json:"-"`
	// HookType is the raw agent hook identifier (e.g. "PreToolUse"). The
	// decision service looks up its HookSpec to set Phase. In-memory only:
	// excluded from JSON and storage.
	HookType HookType `json:"-"`
	// FullContent is the untruncated content read by the AARM content matcher.
	// In-memory only (excluded from JSON, storage, logs); only the short
	// ContentPreview is persisted. Empty for sensitive paths.
	FullContent string `json:"-"`
	// OutputTruncated is true when ObserveOutput cut the tool response to
	// fit FullContent. Mediation copies it to action.content_truncated.
	OutputTruncated bool `json:"-"`
	// IsSensitive is true if path matched sensitive_paths pattern.
	IsSensitive bool `json:"is_sensitive"`
	// SubagentID is set when this event was performed by a subagent (empty for main agent).
	SubagentID string `json:"subagent_id,omitempty"`
	// SubagentType is the type of subagent (e.g., "Explore", "Plan", "general-purpose").
	SubagentType string `json:"subagent_type,omitempty"`
	// Phase is the execution phase of the source hook, from the adapter's
	// HookSpec.
	Phase Phase `json:"phase,omitempty"`
	// Kind is the kind of the event in the session context.
	Kind Kind `json:"kind,omitempty"`
	// ToolCallID is the agent's identifier for one tool call. The pre and
	// post events of one call share it.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// LinkedEventID is the ID of the pre event of the same tool call, set on
	// a post event when Gryph recorded the pre event.
	LinkedEventID uuid.UUID `json:"linked_event_id,omitempty,omitzero"`
	// Origin is where the content of the event came from, as the adapter
	// claims it. ClaimOrigin fills it from the event facts when the adapter
	// does not. The context entry and the content labels store it.
	Origin privacy.Origin `json:"-"`
	// OriginSource names the MCP server when Origin is OriginMCP.
	OriginSource string `json:"-"`
}

// NewEvent creates a new Event with a generated UUID and current timestamp.
func NewEvent(sessionID uuid.UUID, agentName string, actionType ActionType) *Event {
	return &Event{
		ID:           uuid.New(),
		SessionID:    sessionID,
		Timestamp:    time.Now().UTC(),
		AgentName:    agentName,
		ActionType:   actionType,
		ResultStatus: ResultSuccess,
	}
}

// FileReadPayload represents the payload for file_read actions.
type FileReadPayload struct {
	Path        string `json:"path"`
	Pattern     string `json:"pattern,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// DisplayTarget returns the best available identifier for display purposes.
// It prefers Path, falling back to Pattern for tools like Glob/Grep that
// may only have a search pattern and no explicit directory.
func (p *FileReadPayload) DisplayTarget() string {
	if p.Path != "" {
		return p.Path
	}
	return p.Pattern
}

// FileWritePayload represents the payload for file_write actions.
type FileWritePayload struct {
	Path           string       `json:"path"`
	SizeBytes      int64        `json:"size_bytes,omitempty"`
	ContentHash    string       `json:"content_hash,omitempty"`
	ContentPreview privacy.Text `json:"content_preview,omitzero"`
	OldString      privacy.Text `json:"old_string,omitzero"`
	NewString      privacy.Text `json:"new_string,omitzero"`
	LinesAdded     int          `json:"lines_added,omitempty"`
	LinesRemoved   int          `json:"lines_removed,omitempty"`
}

// FileDeletePayload represents the payload for file_delete actions.
type FileDeletePayload struct {
	Path string `json:"path"`
}

// CommandExecPayload represents the payload for command_exec actions.
type CommandExecPayload struct {
	Command       privacy.Text `json:"command"`
	Description   string       `json:"description,omitempty"`
	Args          []string     `json:"args,omitempty"`
	ExitCode      int          `json:"exit_code"`
	Output        privacy.Text `json:"output,omitzero"`
	DurationMs    int64        `json:"duration_ms,omitempty"`
	StdoutPreview privacy.Text `json:"stdout_preview,omitzero"`
	StderrPreview privacy.Text `json:"stderr_preview,omitzero"`
}

// ToolUsePayload represents the payload for tool_use actions.
// Input and Output hold the tool's JSON as a string value. Readers that
// need the structure parse Value.
type ToolUsePayload struct {
	ToolName      string       `json:"tool_name"`
	Input         privacy.Text `json:"input,omitzero"`
	Output        privacy.Text `json:"output,omitzero"`
	OutputPreview privacy.Text `json:"output_preview,omitzero"`
}

// SessionPayload represents the payload for session_start actions.
type SessionPayload struct {
	Source    string `json:"source,omitempty"`
	Model     string `json:"model,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// SessionEndPayload represents the payload for session_end actions.
type SessionEndPayload struct {
	Reason string `json:"reason,omitempty"`
}

// NotificationPayload represents the payload for notification actions.
type NotificationPayload struct {
	Message string          `json:"message,omitempty"`
	Type    string          `json:"type,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

// SubagentStartPayload represents the payload for subagent_start actions.
type SubagentStartPayload struct {
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
}

// SubagentStopPayload represents the payload for subagent_stop actions.
type SubagentStopPayload struct {
	AgentID              string       `json:"agent_id"`
	AgentType            string       `json:"agent_type"`
	AgentTranscriptPath  string       `json:"agent_transcript_path,omitempty"`
	LastAssistantMessage privacy.Text `json:"last_assistant_message,omitzero"`
}

// UserPromptPayload represents the payload for user_prompt events.
type UserPromptPayload struct {
	Prompt privacy.Text `json:"prompt"`
}

// toolUseDisplayFields lists Input keys checked in priority order by DisplayTarget.
var toolUseDisplayFields = []string{
	"url", "query", "command", "file_path", "path",
	"subject", "description", "subagent_type",
	"prompt", "text", "message",
}

// DisplayTarget returns the most relevant identifier from Input for display.
// It checks a prioritised list of well-known fields (url, query, command, …)
// and returns the first non-empty string value found.
func (p *ToolUsePayload) DisplayTarget() string {
	if p.Input.Value == "" {
		return ""
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(p.Input.Value), &m); err != nil {
		return ""
	}

	for _, key := range toolUseDisplayFields {
		raw, ok := m[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if s != "" {
			return s
		}
	}
	return ""
}

// GetToolUsePayload unmarshals the payload as a ToolUsePayload.
func (e *Event) GetToolUsePayload() (*ToolUsePayload, error) {
	if e.ActionType != ActionToolUse {
		return nil, nil
	}
	var payload ToolUsePayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// MarshalJSON implements json.Marshaler to include $schema in the JSON output.
func (e Event) MarshalJSON() ([]byte, error) {
	// Alias avoids infinite recursion by stripping MarshalJSON from the type.
	type eventAlias Event
	return json.Marshal(struct {
		Schema string `json:"$schema"`
		eventAlias
	}{
		Schema:     EventSchemaURL,
		eventAlias: eventAlias(e),
	})
}

// SetPayload marshals the given payload and sets it on the event.
func (e *Event) SetPayload(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = data
	return nil
}

// GetFileReadPayload unmarshals the payload as a FileReadPayload.
func (e *Event) GetFileReadPayload() (*FileReadPayload, error) {
	if e.ActionType != ActionFileRead {
		return nil, nil
	}
	var payload FileReadPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetFileWritePayload unmarshals the payload as a FileWritePayload.
func (e *Event) GetFileWritePayload() (*FileWritePayload, error) {
	if e.ActionType != ActionFileWrite {
		return nil, nil
	}
	var payload FileWritePayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetCommandExecPayload unmarshals the payload as a CommandExecPayload.
func (e *Event) GetCommandExecPayload() (*CommandExecPayload, error) {
	if e.ActionType != ActionCommandExec {
		return nil, nil
	}
	var payload CommandExecPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetFileDeletePayload unmarshals the payload as a FileDeletePayload.
func (e *Event) GetFileDeletePayload() (*FileDeletePayload, error) {
	if e.ActionType != ActionFileDelete {
		return nil, nil
	}
	var payload FileDeletePayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetNotificationPayload unmarshals the payload as a NotificationPayload.
func (e *Event) GetNotificationPayload() (*NotificationPayload, error) {
	if e.ActionType != ActionNotification {
		return nil, nil
	}
	var payload NotificationPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// NewPayload returns a pointer to the zero payload struct of an action type.
// It returns nil when the action type has no payload struct.
func NewPayload(t ActionType) any {
	switch t {
	case ActionFileRead:
		return &FileReadPayload{}
	case ActionFileWrite:
		return &FileWritePayload{}
	case ActionFileDelete:
		return &FileDeletePayload{}
	case ActionCommandExec:
		return &CommandExecPayload{}
	case ActionToolUse:
		return &ToolUsePayload{}
	case ActionSessionStart:
		return &SessionPayload{}
	case ActionSessionEnd:
		return &SessionEndPayload{}
	case ActionNotification:
		return &NotificationPayload{}
	case ActionSubagentStart:
		return &SubagentStartPayload{}
	case ActionSubagentStop:
		return &SubagentStopPayload{}
	case ActionUserPrompt:
		return &UserPromptPayload{}
	default:
		return nil
	}
}

// DecodePayload decodes the payload into the typed struct of the action type.
// It returns nil when the payload is empty or the action type has no struct.
func (e *Event) DecodePayload() (any, error) {
	p := NewPayload(e.ActionType)
	if p == nil || len(e.Payload) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(e.Payload, p); err != nil {
		return nil, err
	}
	return p, nil
}

// toolTargetKeys are the tool input keys that name a file.
var toolTargetKeys = []string{"file_path", "path", "notebook_path"}

// Targets returns the paths and the URL that the event acts on. The
// classifier reads them.
func (e *Event) Targets() (paths []string, url string) {
	switch e.ActionType {
	case ActionFileRead, ActionFileWrite, ActionFileDelete:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(e.Payload, &p); err == nil && p.Path != "" {
			paths = append(paths, p.Path)
		}
	case ActionToolUse:
		p, err := e.GetToolUsePayload()
		if err != nil || p == nil || p.Input.Value == "" {
			return nil, ""
		}
		var input map[string]any
		if err := json.Unmarshal([]byte(p.Input.Value), &input); err != nil {
			return nil, ""
		}
		for _, k := range toolTargetKeys {
			if v, ok := input[k].(string); ok && v != "" {
				paths = append(paths, v)
			}
		}
		url, _ = input["url"].(string)
	}
	return paths, url
}

// ContentDigest returns one digest for the content of the event. It is the
// label digest of the single content value, or the digest of the list of
// label digests when the event has more than one value. A label digest is
// taken before redaction. It returns an empty string when no value has a
// digest.
func (e *Event) ContentDigest() string {
	var digests []string
	add := func(_ string, t *privacy.Text) {
		if t.Label.Digest != "" {
			digests = append(digests, t.Label.Digest)
		}
	}
	add("diff_content", &e.DiffContent)
	p, err := e.DecodePayload()
	if err != nil {
		log.Warnf("content digest: decode %s payload: %v", e.ActionType, err)
	}
	if p != nil {
		privacy.Walk(p, add)
	}
	switch len(digests) {
	case 0:
		return ""
	case 1:
		return digests[0]
	default:
		return privacy.Digest(strings.Join(digests, "\n"))
	}
}

// SetPrompt sets the payload of a user_prompt event. The origin is user for
// a prompt that a person typed, and agent for a prompt that agent code
// injected. FullContent carries the whole prompt for content rules.
func (e *Event) SetPrompt(prompt string, origin privacy.Origin) error {
	e.FullContent = prompt
	e.Origin = origin
	return e.SetPayload(UserPromptPayload{
		Prompt: privacy.Text{Value: prompt, Label: privacy.Label{Origin: origin}},
	})
}

// GetUserPromptPayload unmarshals the payload as a UserPromptPayload.
func (e *Event) GetUserPromptPayload() (*UserPromptPayload, error) {
	if e.ActionType != ActionUserPrompt {
		return nil, nil
	}
	var payload UserPromptPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
