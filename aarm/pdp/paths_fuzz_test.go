package pdp

import (
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/internal/fuzzseed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzGlobsOverlap checks that the path matcher never panics, stays in its
// time bound, and gives one result for one input. A path with no glob
// character overlaps itself. Run it with:
//
//	go test -run '^$' -fuzz=FuzzGlobsOverlap -fuzztime=10m ./aarm/pdp/
func FuzzGlobsOverlap(f *testing.F) {
	literals := fuzzseed.StringLiterals(f, "pdp_test.go", "p0_test.go", "../loader/builtin_test.go")
	var paths []string
	for _, s := range literals {
		if strings.ContainsAny(s, "/*") {
			paths = append(paths, s)
		}
	}
	for i, p := range paths {
		f.Add(p, p, true)
		f.Add(p, paths[(i+1)%len(paths)], i%2 == 0)
	}
	f.Fuzz(func(t *testing.T, glob, pattern string, matchDot bool) {
		start := time.Now()
		got := globsOverlap(glob, []string{pattern}, matchDot)
		require.Less(t, time.Since(start), time.Second)
		assert.Equal(t, got, globsOverlap(glob, []string{pattern}, matchDot), "the match is deterministic")
		if !strings.ContainsAny(glob, "*?[{") {
			assert.True(t, globsOverlap(glob, []string{glob}, matchDot), "a literal path overlaps itself")
		}
	})
}
