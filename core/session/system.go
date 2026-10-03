package session

import "github.com/google/uuid"

// SystemAgentName is the agent name of a system session. Gryph itself is
// the actor in it.
const SystemAgentName = "gryph"

const systemSessionPrefix = "gryph-system:"

// SystemSessionID returns the ID of the system session of one account. It
// is deterministic, so every run of Gryph for that account appends to one
// receipt chain, in the same way an agent session derives its ID from the
// agent's session identifier.
func SystemSessionID(accountID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(systemSessionPrefix+accountID))
}

// NewSystemSession returns the system session of one account. It holds the
// events that Gryph records outside any agent session, such as tamper
// events.
func NewSystemSession(accountID string) *Session {
	s := NewSessionWithID(SystemSessionID(accountID), SystemAgentName)
	s.AgentSessionID = systemSessionPrefix + accountID
	return s
}
