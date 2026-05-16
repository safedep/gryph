// Package pdp fuzz harness.
//
// Run the parser fuzzer with mutation enabled:
//
//	go test -fuzz=FuzzParsePolicy -fuzztime=30s ./aarm/pdp/
//
// Run the CEL condition fuzzer:
//
//	go test -fuzz=FuzzCELCondition -fuzztime=30s ./aarm/pdp/
//
// The seed corpus runs unconditionally in standard `go test ./aarm/pdp/`.
// Mutation-based fuzzing only runs when the -fuzz flag is set.

package pdp

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/model"
)

// FuzzParsePolicy feeds arbitrary bytes to ParsePolicy. The property is that
// the parser never panics and always returns either a valid policy or a
// non-nil error (never both nil and never both non-nil).
func FuzzParsePolicy(f *testing.F) {
	// Seed 1: a clean, fully-valid policy YAML.
	f.Add([]byte(`version: "1"
rules:
  - id: clean-rule
    action: guidance
    severity: medium
    match:
      action_types: [file_read]
      file_patterns:
        - "**/.env"
    message: "clean seed"
`))

	// Seed 2: a mildly malformed policy missing a required field (no id).
	f.Add([]byte(`version: "1"
rules:
  - action: guidance
    severity: medium
`))

	// Seed 3: an adversarial input that exercises the regex compile path
	// with a classic catastrophic-backtracking pattern. The earlier seed
	// here paired this with a 16KiB literal content_pattern AND a duplicate
	// rule id, which caused ParsePolicy to fail at the duplicate-id check
	// before ever reaching regex compilation, defeating the intent. This
	// seed is a single valid rule so the parser must compile each
	// content_pattern. The "(a+)+$" pattern is a textbook ReDoS-shaped
	// regex. Whether Go's RE2 backend backtracks is irrelevant. The
	// purpose is to exercise the compile path with adversarially-shaped
	// input that a future regex-engine swap might be more sensitive to.
	f.Add([]byte(`version: "1"
rules:
  - id: adversarial-regex
    action: block
    severity: critical
    match:
      action_types: [file_write]
      content_patterns:
        - "(a+)+$"
        - "(a|aa)+$"
`))

	f.Fuzz(func(t *testing.T, data []byte) {
		policy, err := ParsePolicy(data)
		if err == nil && policy == nil {
			t.Fatalf("ParsePolicy returned (nil, nil) on len=%d", len(data))
		}
		if err != nil && policy != nil {
			t.Fatalf("ParsePolicy returned both a policy and an error on len=%d", len(data))
		}
	})
}

// FuzzCELCondition takes a string condition, wraps it in a minimal valid
// policy YAML, and feeds it through ParsePolicy + Evaluate with a synthetic
// action. The property is that no panic ever escapes and that evaluation
// completes within a small multiple of the existing condition timeout
// (conditionTimeout, 100ms). A leak of goroutines spawned by Evaluate would
// also surface as a high delta in runtime.NumGoroutine over the run.
func FuzzCELCondition(f *testing.F) {
	// Seed 1: a clean, valid CEL expression.
	f.Add(`action.params.lines_added > 100 && context.total_actions >= 3`)

	// Seed 2: a syntactically malformed expression (unterminated string).
	f.Add(`action.params.path == "open`)

	// Seed 3: an adversarial expression mixing deep nesting, a long literal
	// chain, and a backtracking-prone string operation. The CEL runtime
	// enforces a cost cap (10000) but the parse path should not panic.
	f.Add(`action.params.path.contains("` + strings.Repeat("a", 4096) + `") && (1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1) > 0`)

	f.Fuzz(func(t *testing.T, expr string) {
		policyYAML := fmt.Sprintf(`version: "1"
rules:
  - id: fuzz-rule
    action: guidance
    severity: low
    match:
      action_types: [file_write]
    condition: %q
`, expr)

		policy, err := ParsePolicy([]byte(policyYAML))
		if err != nil {
			// Parse failure is a valid outcome for arbitrary expressions and
			// is not a fuzz signal.
			return
		}
		if policy == nil {
			t.Fatalf("ParsePolicy returned (nil, nil) for expr=%q", expr)
		}

		engine, err := New(policy)
		if err != nil {
			return
		}

		baseline := runtime.NumGoroutine()
		action := &model.Action{
			Type:       model.ActionFileWrite,
			Tool:       "Write",
			Agent:      "claude-code",
			Parameters: model.Parameters{Path: "/tmp/file.go", LinesAdded: 250},
		}
		snapshot := &model.ContextSnapshot{TotalActions: 5}

		// Hard ceiling: 5x conditionTimeout (100ms) covers GC stalls and CI
		// jitter while still catching a runaway evaluation. The evaluation
		// path also enforces conditionTimeout internally via
		// context.WithTimeout.
		ctx, cancel := context.WithTimeout(context.Background(), 5*conditionTimeout)

		done := make(chan struct{})
		var evalErr error
		go func() {
			defer close(done)
			_, evalErr = engine.Evaluate(ctx, action, snapshot)
		}()
		select {
		case <-done:
			// Evaluation completed (either successfully or with an error).
			// A returned error is fine, a panic would have already failed
			// the test before reaching here. evalErr is used to silence
			// the unused-write linter without changing behavior.
			_ = evalErr
		case <-time.After(5 * conditionTimeout):
			cancel()
			t.Fatalf("evaluation exceeded the 5x conditionTimeout ceiling for expr=%q", expr)
		}

		// Cancel the context before sampling NumGoroutine so the
		// context.WithTimeout timer goroutine is released and does not
		// pollute the leak count. Without this, the timer routine
		// remains alive for the duration of the test body and the
		// baseline-vs-current delta is measured against a misleading
		// snapshot. runtime.Gosched yields so the runtime can finalise
		// the timer cleanup before we sample.
		cancel()
		runtime.Gosched()

		const goroutineLeakSlack = 5
		current := runtime.NumGoroutine()
		if current > baseline+goroutineLeakSlack {
			t.Fatalf("goroutine count grew from %d to %d for expr=%q", baseline, current, expr)
		}
	})
}
