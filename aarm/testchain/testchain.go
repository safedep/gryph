// Package testchain houses property-test scaffolding shared by the
// receipt and context chain property tests. The helpers exist as a
// regular (non _test) package so they can be imported from multiple
// _test.go files across packages. No production code depends on this
// package.
package testchain

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"
)

// PropertyMaxCount is the cap on cases per property. Big enough that the
// distribution of generated inputs exercises edge cases without slowing the
// regular test suite.
const PropertyMaxCount = 200

// PropertyConfig builds a quick.Config wired to a freshly seeded PRNG and
// logs the seed up front so test failures are reproducible.
func PropertyConfig(t *testing.T) *quick.Config {
	t.Helper()
	seed := time.Now().UnixNano()
	t.Logf("property seed: %d", seed)
	return &quick.Config{
		MaxCount: PropertyMaxCount,
		Rand:     rand.New(rand.NewSource(seed)),
	}
}

// ChainSize is a quick.Generator for chain length in [1, 20]. The default
// int generator would explode the range and most cases would degrade to
// "build a 10 million row chain in memory".
type ChainSize int

// Generate implements quick.Generator.
func (ChainSize) Generate(rand *rand.Rand, _ int) reflect.Value {
	return reflect.ValueOf(ChainSize(rand.Intn(20) + 1))
}

// PrevHashTamperCase is a randomised prev-hash tamper case. Size is at least
// 2 because index 0 has no preceding row and therefore no prev_hash chain
// link to break. Row indexes into [1, Size). Kind picks one of two tamper
// modes (0 mutates the shared backing slice, 1 reassigns the row's PrevHash
// to a new buffer).
type PrevHashTamperCase struct {
	Size int
	Row  int
	Kind int
}

// Generate implements quick.Generator.
func (PrevHashTamperCase) Generate(rand *rand.Rand, _ int) reflect.Value {
	size := rand.Intn(19) + 2
	return reflect.ValueOf(PrevHashTamperCase{
		Size: size,
		Row:  rand.Intn(size-1) + 1,
		Kind: rand.Intn(2),
	})
}
