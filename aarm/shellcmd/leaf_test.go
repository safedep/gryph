package shellcmd

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoGryphImports keeps shellcmd a leaf package. aarm/model imports it,
// so a Gryph import here can create an import cycle.
func TestNoGryphImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			assert.False(t, strings.HasPrefix(path, "github.com/safedep/gryph"), "%s imports %s", f, path)
		}
	}
}
