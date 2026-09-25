package accumulator

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNop(t *testing.T) {
	var n Accumulator = NewNop()
	ctx := context.Background()
	require.NoError(t, n.Append(ctx, &model.ContextEntry{ID: uuid.New()}))
	require.NoError(t, n.RecordResult(ctx, uuid.New(), model.Result{}))

	a, err := n.Snapshot(ctx, uuid.New(), &model.ContextEntry{ActionType: model.ActionFileRead})
	require.NoError(t, err)
	assert.Zero(t, a.TotalActions)
	a.TotalActions = 99

	b, err := n.Snapshot(ctx, uuid.New(), nil)
	require.NoError(t, err)
	assert.Zero(t, b.TotalActions, "a snapshot must not see an earlier caller mutation")
}
