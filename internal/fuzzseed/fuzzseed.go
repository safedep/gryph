// Package fuzzseed reads fuzz seeds from the table tests of a package.
package fuzzseed

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// StringLiterals returns each distinct string literal in the Go files, in
// file order. A table test holds its inputs as string literals, so the fuzz
// corpus grows when a table test grows.
func StringLiterals(tb testing.TB, files ...string) []string {
	tb.Helper()
	seen := map[string]bool{}
	var out []string
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		require.NoError(tb, err)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			require.NoError(tb, err)
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
			return true
		})
	}
	return out
}
