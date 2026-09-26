package security

import (
	"context"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
)

type fixedCheck struct{ result *CheckResult }

func (fixedCheck) Name() string  { return "fixed" }
func (fixedCheck) Enabled() bool { return true }
func (c fixedCheck) Check(context.Context, *events.Event, *session.Session) (*CheckResult, error) {
	return c.result, nil
}

func TestEvaluator_StoredBlockReason(t *testing.T) {
	cases := []struct {
		name       string
		check      *CheckResult
		wantStored string
	}{
		{"stored reason set", &CheckResult{Decision: DecisionBlock, Reason: "full", StoredReason: "stored"}, "stored"},
		{"stored reason empty uses the reason", &CheckResult{Decision: DecisionBlock, Reason: "full"}, "full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New(nil)
			e.RegisterCheck(fixedCheck{result: tc.check})
			got := e.Evaluate(context.Background(), &events.Event{}, nil)
			assert.Equal(t, "full", got.BlockReason)
			assert.Equal(t, tc.wantStored, got.StoredBlockReason)
		})
	}
}
