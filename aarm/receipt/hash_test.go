package receipt

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The consensus format hashes matched_rule_ids as "null" when the list is
// empty. The PDP produces an empty non-nil slice for a no-match decision,
// while storage returns nil for the same row. Both must hash identically or
// every allow receipt breaks chain verification after a DB or export round
// trip.
func TestNewHashInputNormalizesEmptyMatchedRuleIDs(t *testing.T) {
	base := HashInputFields{
		Sequence:       1,
		RecordedAtUnix: time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC).UnixNano(),
		SessionID:      uuid.MustParse("2fad3402-a209-56c0-8c76-25256098cf39"),
		Agent:          "claude-code",
		Tool:           "Read",
		ActionType:     "file_read",
		Decision:       "allow",
	}

	withNil := base
	withNil.MatchedRuleIDs = nil
	withEmpty := base
	withEmpty.MatchedRuleIDs = []string{}
	withRule := base
	withRule.MatchedRuleIDs = []string{"r-1"}

	hashNil, err := ComputeHash(NewHashInput(withNil))
	require.NoError(t, err)
	hashEmpty, err := ComputeHash(NewHashInput(withEmpty))
	require.NoError(t, err)
	hashRule, err := ComputeHash(NewHashInput(withRule))
	require.NoError(t, err)

	assert.Equal(t, hashNil, hashEmpty, "nil and empty matched_rule_ids must hash identically")
	assert.NotEqual(t, hashNil, hashRule, "a matched rule must change the hash")
}
