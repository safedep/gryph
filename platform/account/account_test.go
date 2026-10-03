package account

import (
	"math"
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

func TestChown_RefusesAnIdentifierOutOfRange(t *testing.T) {
	acct := Account{Name: "wide", UID: math.MaxUint32, GID: 1}
	err := acct.Chown(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of the range")
}
