package receipt

import (
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/privacy"
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

func TestComputeHash_Versions(t *testing.T) {
	base := HashInputFields{
		Sequence:       1,
		RecordedAtUnix: time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC).UnixNano(),
		SessionID:      uuid.MustParse("2fad3402-a209-56c0-8c76-25256098cf39"),
		ActionType:     "command_exec",
		Decision:       "allow",
		ActionPayload:  map[string]interface{}{"command": "curl https://example.com", "url": "https://example.com", "working_dir": "/work"},
		CommandDigest:  privacy.Digest("curl https://example.com"),
		URLDigest:      privacy.Digest("https://example.com"),
	}
	hash := func(f HashInputFields) []byte {
		h, err := ComputeHash(NewHashInput(f))
		require.NoError(t, err)
		return h
	}
	with := func(mutate func(*HashInputFields)) HashInputFields {
		f := base
		f.ActionPayload = maps.Clone(base.ActionPayload)
		mutate(&f)
		return f
	}
	v1 := with(func(f *HashInputFields) { f.HashVersion = HashV1 })
	v2 := with(func(f *HashInputFields) { f.HashVersion = HashV2 })

	assert.Equal(t, hash(v1), hash(with(func(f *HashInputFields) { f.HashVersion = 0 })), "a row without hash_version is v1")
	assert.NotEqual(t, hash(v1), hash(v2))

	t.Run("v1 covers the command and ignores the digests", func(t *testing.T) {
		assert.NotEqual(t, hash(v1), hash(with(func(f *HashInputFields) { f.HashVersion = HashV1; delete(f.ActionPayload, "command") })))
		assert.Equal(t, hash(v1), hash(with(func(f *HashInputFields) { f.HashVersion = HashV1; f.CommandDigest = "" })))
	})

	t.Run("v2 covers the digests and ignores the values", func(t *testing.T) {
		assert.Equal(t, hash(v2), hash(with(func(f *HashInputFields) {
			f.HashVersion = HashV2
			delete(f.ActionPayload, "command")
			f.ActionPayload["url"] = privacy.RedactedValue
		})))
		assert.NotEqual(t, hash(v2), hash(with(func(f *HashInputFields) { f.HashVersion = HashV2; f.CommandDigest = "" })))
		assert.NotEqual(t, hash(v2), hash(with(func(f *HashInputFields) { f.HashVersion = HashV2; f.URLDigest = "" })))
		assert.NotEqual(t, hash(v2), hash(with(func(f *HashInputFields) { f.HashVersion = HashV2; f.ActionPayload["working_dir"] = "/other" })))
	})

	t.Run("v2 hashes a payload with only content as no payload", func(t *testing.T) {
		only := with(func(f *HashInputFields) {
			f.HashVersion = HashV2
			f.ActionPayload = map[string]interface{}{"command": "ls"}
		})
		none := with(func(f *HashInputFields) { f.HashVersion = HashV2; f.ActionPayload = nil })
		assert.Equal(t, hash(only), hash(none))
	})

	t.Run("unknown version", func(t *testing.T) {
		_, err := ComputeHash(NewHashInput(with(func(f *HashInputFields) { f.HashVersion = 3 })))
		assert.ErrorContains(t, err, "unknown hash version 3")
	})
}
