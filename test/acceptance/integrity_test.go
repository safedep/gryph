package acceptance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogIntegrity runs under normal `go test ./...`. It fails when a
// script has no catalog row, so a typo cannot become an untracked guarantee.
// A catalog row with no script is a gap. The suite reports it and never fails
// the build for it.
func TestCatalogIntegrity(t *testing.T) {
	cat, err := LoadCatalog("catalog.yaml")
	require.NoError(t, err)

	scriptIDs, err := DiscoverScripts("scripts")
	require.NoError(t, err)
	require.NotEmpty(t, scriptIDs)

	for _, id := range scriptIDs {
		assert.Truef(t, cat.Has(id),
			"script %q.txtar has no catalog.yaml entry. Add one so it cannot become a phantom guarantee", id)
	}
}
