package cli

import (
	"testing"
	"time"

	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
)

// The client waits for an inline answer no longer than the configured
// wait, and never past the hook timeout of the agent minus the margin. A
// budget under the minimum is no wait.
func TestInlineWait(t *testing.T) {
	cases := []struct {
		name       string
		configured time.Duration
		hook       time.Duration
		want       time.Duration
	}{
		{"configured wait under a long hook timeout", 15 * time.Second, 600 * time.Second, 15 * time.Second},
		{"the hook timeout bounds the wait", 15 * time.Second, 5 * time.Second, 4500 * time.Millisecond},
		{"a short hook timeout skips the wait", 15 * time.Second, 2 * time.Second, 0},
		{"a short configured wait skips the wait", time.Second, 600 * time.Second, 0},
		{"no documented hook timeout keeps the configured wait", 15 * time.Second, 0, 15 * time.Second},
		{"no configured wait takes the default", 0, 600 * time.Second, config.DefaultApprovalInlineWait},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, inlineWait(tc.configured, tc.hook))
		})
	}
}
