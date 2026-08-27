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

	covered := make(map[string]bool, len(scriptIDs))
	for _, id := range scriptIDs {
		covered[id] = true
		assert.Truef(t, cat.Has(id),
			"script %q.txtar has no catalog.yaml entry. Add one so it cannot become a phantom guarantee", id)
	}

	// A catalog row with no script is a gap. Report it so a renamed or
	// deleted script cannot silently keep its coverage claim, but never fail
	// the build for it.
	for _, id := range cat.IDs() {
		if !covered[id] {
			t.Logf("gap: catalog entry %q has no script under scripts/", id)
		}
	}
}
