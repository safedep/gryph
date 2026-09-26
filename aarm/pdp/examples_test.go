package pdp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExamplePolicies_Compile(t *testing.T) {
	files, err := filepath.Glob("../../examples/policies/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			policy, err := ParsePolicy(data)
			require.NoError(t, err)
			_, err = New(policy)
			assert.NoError(t, err)
		})
	}
}
