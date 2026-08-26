package aarmconformance_test

import (
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
)

func TestR7_SemanticDistanceComputed(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance computed between original and revised intent")
	aarm.Skip(t, aarm.OutOfScope, "embedding-based semantic distance is not in Gryph's roadmap")
}

func TestR7_SemanticDistanceSurfacedToPolicy(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance surfaced to policy")
	aarm.Skip(t, aarm.OutOfScope, "semantic_drift remains 0 by design until embeddings are wired")
}

func TestR7_SemanticDistanceLoggedOnReceipt(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance logged on receipt")
	aarm.Skip(t, aarm.OutOfScope, "no semantic-drift column until R7 is in scope")
}

func TestR7_SemanticDistanceThresholdConfigurable(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance threshold configurable")
	aarm.Skip(t, aarm.OutOfScope, "no threshold knob until R7 is in scope")
}

func TestR7_SemanticDistanceTriggersDefer(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance > threshold triggers defer")
	aarm.Skip(t, aarm.OutOfScope, "no automatic defer-on-drift trigger until R7 is in scope")
}

func TestR7_SemanticDistanceComputeBudgeted(t *testing.T) {
	aarm.Requires(t, aarm.R7, aarm.SHOULD, "Semantic distance compute is budget-bounded")
	aarm.Skip(t, aarm.OutOfScope, "compute budget irrelevant until R7 lands")
}
