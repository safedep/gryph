package accumulator

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNop_SatisfiesInterface(t *testing.T) {
	var _ Accumulator = (*Nop)(nil)
	var _ Accumulator = NewNop()
}

func TestNop_AppendReturnsNil(t *testing.T) {
	n := NewNop()
	require.NoError(t, n.Append(context.Background(), &model.Action{ID: uuid.New()}))
	require.NoError(t, n.Append(context.Background(), nil))
}

func TestNop_RecordResultReturnsNil(t *testing.T) {
	require.NoError(t, NewNop().RecordResult(context.Background(), uuid.New(), model.Result{}))
}

func TestNop_SnapshotReturnsEmpty(t *testing.T) {
	snap, err := NewNop().Snapshot(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, 0, snap.TotalActions)
	assert.Equal(t, 0, snap.FilesRead)
	assert.Empty(t, snap.ToolsUsed)
}

func TestNop_SnapshotsAreIndependent(t *testing.T) {
	n := NewNop()
	a, err := n.Snapshot(context.Background(), uuid.New())
	require.NoError(t, err)
	a.TotalActions = 99
	b, err := n.Snapshot(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 0, b.TotalActions, "subsequent snapshots must not see prior caller mutations")
}
