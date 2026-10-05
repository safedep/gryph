package session

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSystemSessionID(t *testing.T) {
	assert.Equal(t, SystemSessionID("1000"), SystemSessionID("1000"), "deterministic")
	assert.NotEqual(t, SystemSessionID("1000"), SystemSessionID("1001"), "one session per account")
	assert.Equal(t, uuid.NewSHA1(uuid.NameSpaceOID, []byte("gryph-system:1000")), SystemSessionID("1000"))
}

func TestNewSystemSession(t *testing.T) {
	s := NewSystemSession("1000")
	assert.Equal(t, SystemSessionID("1000"), s.ID)
	assert.Equal(t, SystemAgentName, s.AgentName)
	assert.Equal(t, "gryph-system:1000", s.AgentSessionID)
	assert.True(t, s.IsActive())
}
