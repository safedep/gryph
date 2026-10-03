package account

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentID(t *testing.T) {
	id, err := CurrentID()
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	again, err := CurrentID()
	require.NoError(t, err)
	assert.Equal(t, id, again, "the identifier is stable within one process")
}
