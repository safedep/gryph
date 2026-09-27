package shellcmd

import (
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/internal/fuzzseed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

// fuzzTimeBound is the time one analysis may take under the fuzzer. It is
// far above the hook budget, because the fuzzer runs one worker per CPU.
const fuzzTimeBound = 2 * time.Second

// fuzzMaxInput skips larger inputs. A long valid command can take longer
// than fuzzTimeBound without a bug. The hook budget covers such a command,
// and the benchmarks measure large shapes.
const fuzzMaxInput = 32 << 10

// FuzzAnalyze checks that the analysis never panics, stays in its time
// bound, and gives one result for one input. The walker runs without the
// recover of Analyze, so a panic fails the fuzz run. Run it with:
//
//	go test -run '^$' -fuzz=FuzzAnalyze -fuzztime=10m ./aarm/shellcmd/
func FuzzAnalyze(f *testing.F) {
	for _, s := range fuzzseed.StringLiterals(f, "analyze_test.go", "shellcmd_test.go", "leaf_test.go", "../loader/builtin_test.go") {
		f.Add(s)
	}
	for _, s := range benchShapes {
		f.Add(s.command(min(s.size, 8)))
	}
	f.Fuzz(func(t *testing.T, command string) {
		if len(command) > fuzzMaxInput {
			t.Skip()
		}
		start := time.Now()
		a := (&walker{env: testEnv}).analyze(command)
		require.Less(t, time.Since(start), fuzzTimeBound)

		assert.Equal(t, a, (&walker{env: testEnv}).analyze(command), "the analysis is deterministic")
		if _, err := syntax.NewParser().Parse(strings.NewReader(command), ""); err != nil {
			assert.False(t, a.Parsed, "a command that does not parse is not parsed")
		}
	})
}
