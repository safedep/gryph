// Package events provides the core event model and types for audit logging.
package events

import "fmt"

// ActionType represents the type of action performed by an agent.
type ActionType string

const (
	// ActionFileRead indicates an agent read a file.
	ActionFileRead ActionType = "file_read"
	// ActionFileWrite indicates an agent wrote/modified a file.
	ActionFileWrite ActionType = "file_write"
	// ActionFileDelete indicates an agent deleted a file.
	ActionFileDelete ActionType = "file_delete"
	// ActionCommandExec indicates an agent executed a shell command.
	ActionCommandExec ActionType = "command_exec"
	// ActionNetworkRequest indicates an agent made an HTTP request.
	ActionNetworkRequest ActionType = "network_request"
	// ActionToolUse indicates other tool invocation.
	ActionToolUse ActionType = "tool_use"
	// ActionSessionStart indicates a session started.
	ActionSessionStart ActionType = "session_start"
	// ActionSessionEnd indicates a session ended.
	ActionSessionEnd ActionType = "session_end"
	// ActionNotification indicates a notification was sent.
	ActionNotification ActionType = "notification"
	// ActionUnknown indicates an unrecognized action type.
	ActionUnknown ActionType = "unknown"
)

// actionDisplayNames is the single source of truth mapping each ActionType
// to its short display name. All lookups (DisplayName, IsValid, ParseActionType)
// are derived from this map.
var actionDisplayNames = map[ActionType]string{
	ActionFileRead:       "read",
	ActionFileWrite:      "write",
	ActionFileDelete:     "delete",
	ActionCommandExec:    "exec",
	ActionNetworkRequest: "http",
	ActionToolUse:        "tool",
	ActionSessionStart:   "session_start",
	ActionSessionEnd:     "session_end",
	ActionNotification:   "notification",
	ActionUnknown:        "unknown",
}

var displayToAction map[string]ActionType

func init() {
	displayToAction = make(map[string]ActionType, len(actionDisplayNames))
	for at, dn := range actionDisplayNames {
		displayToAction[dn] = at
	}
}

// String returns the string representation of an ActionType.
func (a ActionType) String() string {
	return string(a)
}

// IsValid returns true if the ActionType is a known type.
func (a ActionType) IsValid() bool {
	_, ok := actionDisplayNames[a]
	return ok
}

// DisplayName returns a short human-readable name for the action type.
func (a ActionType) DisplayName() string {
	if dn, ok := actionDisplayNames[a]; ok {
		return dn
	}
	return "unknown"
}

// ParseActionType parses a string into an ActionType, accepting both
// full names (e.g. "file_write") and display names (e.g. "write").
func ParseActionType(s string) (ActionType, error) {
	if at, ok := displayToAction[s]; ok {
		return at, nil
	}
	at := ActionType(s)
	if at.IsValid() {
		return at, nil
	}
	return "", fmt.Errorf("invalid action type: %q", s)
}

// ResultStatus represents the outcome of an agent action.
type ResultStatus string

const (
	// ResultSuccess indicates the action completed successfully.
	ResultSuccess ResultStatus = "success"
	// ResultError indicates the action failed with an error.
	ResultError ResultStatus = "error"
	// ResultBlocked indicates the action was blocked by policy.
	ResultBlocked ResultStatus = "blocked"
	// ResultRejected indicates the user rejected the action.
	ResultRejected ResultStatus = "rejected"
)

// String returns the string representation of a ResultStatus.
func (r ResultStatus) String() string {
	return string(r)
}

// IsValid returns true if the ResultStatus is a known status.
func (r ResultStatus) IsValid() bool {
	switch r {
	case ResultSuccess, ResultError, ResultBlocked, ResultRejected:
		return true
	default:
		return false
	}
}
