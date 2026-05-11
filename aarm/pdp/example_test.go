package pdp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleYAML_ParsesAndCompiles(t *testing.T) {
	body := ExampleYAML()
	require.NotEmpty(t, body, "embedded example policy must not be empty")

	policy, err := ParsePolicy([]byte(body))
	require.NoError(t, err, "embedded example policy must parse")
	require.NotNil(t, policy)
	assert.NotEmpty(t, policy.Rules, "embedded example policy must declare rules")

	_, err = New(policy)
	require.NoError(t, err, "embedded example policy must compile")
}
