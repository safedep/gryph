package localauth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecurity stands in for security authorizationdb. It keeps the right.
func fakeSecurity(t *testing.T) *string {
	t.Helper()
	var right string
	restore := security
	security = func(_ context.Context, stdin string, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "authorizationdb read":
			if right == "" {
				return "", errors.New("NO (-60005)")
			}
			return right, nil
		case "authorizationdb write":
			right = stdin
		case "authorizationdb remove":
			right = ""
		}
		return "", nil
	}
	t.Cleanup(func() { security = restore })
	return &right
}

func TestRegister_Darwin(t *testing.T) {
	right := fakeSecurity(t)
	ctx := context.Background()

	reg, err := Register(ctx)
	require.NoError(t, err)
	assert.Equal(t, Registration{Path: "authorizationdb:" + ActionID, Changed: true}, reg)
	assert.Contains(t, *right, "<key>session-owner</key>\n\t<true/>")
	assert.Contains(t, *right, "<key>shared</key>\n\t<false/>")
	assert.Contains(t, *right, "<key>allow-root</key>\n\t<false/>")

	reg, err = Register(ctx)
	require.NoError(t, err)
	assert.False(t, reg.Changed, "a right of this version stays")

	reg, err = Unregister(ctx)
	require.NoError(t, err)
	assert.True(t, reg.Changed)
	assert.Empty(t, *right)
	reg, err = Unregister(ctx)
	require.NoError(t, err)
	assert.False(t, reg.Changed, "a missing right is not an error")
}

func TestAuthorize_DarwinNeedsAProof(t *testing.T) {
	auth := Default()
	ctx := context.Background()
	for name, proof := range map[string]Proof{
		"no proof":       {},
		"another kind":   {Kind: "polkit", Data: make([]byte, 32)},
		"short data":     {Kind: proofKind, Data: make([]byte, 8)},
		"no live object": {Kind: proofKind, Data: []byte(strings.Repeat("x", 32))},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := auth.Authorize(ctx, Request{Action: ActionID, Interactive: true, Proof: proof})
			require.NoError(t, err)
			assert.Equal(t, Result{Challenge: true}, res)
		})
	}
}

func TestHint_Darwin(t *testing.T) {
	assert.Contains(t, Hint(), "desktop session")
}
