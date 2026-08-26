package pdp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicy_HashDeterministic(t *testing.T) {
	body := `version: "1"
rules:
  - id: rule-a
    action: warn
    severity: medium
    tags: [secret]
    match:
      file_patterns: ["**/.env"]
  - id: rule-b
    action: block
    severity: high
    match:
      command_patterns: ["rm -rf /"]
`
	p1, err := ParsePolicy([]byte(body))
	require.NoError(t, err)
	p2, err := ParsePolicy([]byte(body))
	require.NoError(t, err)
	h1 := p1.Hash()
	h2 := p2.Hash()
	require.NotEmpty(t, h1)
	assert.True(t, bytes.Equal(h1, h2), "two parses of the same document must hash identically")
}

func TestPolicy_HashIgnoresWhitespaceAndRuleOrder(t *testing.T) {
	a := `version: "1"
rules:
  - id: rule-a
    action: warn
    severity: medium
    tags: [secret]
  - id: rule-b
    action: block
    severity: high
`
	b := `version: "1"
rules:
  - id: rule-b
    action: block
    severity: high

  - id: rule-a
    action: warn
    severity: medium
    tags: [secret]
`
	pa, err := ParsePolicy([]byte(a))
	require.NoError(t, err)
	pb, err := ParsePolicy([]byte(b))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(pa.Hash(), pb.Hash()), "rule order and whitespace must not affect the hash")
}

func TestPolicy_HashChangesOnDecisionFlip(t *testing.T) {
	before := `version: "1"
rules:
  - id: rule-a
    action: warn
    severity: medium
  - id: rule-b
    action: block
    severity: high
`
	after := `version: "1"
rules:
  - id: rule-a
    action: allow
    severity: medium
  - id: rule-b
    action: block
    severity: high
`
	pa, err := ParsePolicy([]byte(before))
	require.NoError(t, err)
	pb, err := ParsePolicy([]byte(after))
	require.NoError(t, err)
	assert.False(t, bytes.Equal(pa.Hash(), pb.Hash()), "flipping a rule decision must change the hash")
}

func TestPolicy_HashCached(t *testing.T) {
	body := `version: "1"
rules:
  - id: rule-a
    action: warn
    severity: medium
`
	p, err := ParsePolicy([]byte(body))
	require.NoError(t, err)
	first := p.Hash()
	second := p.Hash()
	assert.True(t, bytes.Equal(first, second))
}

func TestPolicy_HashNilSafe(t *testing.T) {
	var p *Policy
	assert.Nil(t, p.Hash())
}
