package fuzzseed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringLiterals(t *testing.T) {
	file := filepath.Join(t.TempDir(), "table_test.go")
	src := "package x\n\nvar cases = []string{\"rm a\", `cat \"b\"`, \"rm a\", \"\"}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))

	assert.Equal(t, []string{"rm a", `cat "b"`}, StringLiterals(t, file))
}
