package localauth

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

func TestUnregister_DarwinReportsAFailedRead(t *testing.T) {
	restore := security
	security = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("security: permission denied")
	}
	t.Cleanup(func() { security = restore })
	_, err := Unregister(context.Background())
	assert.Error(t, err, "only a missing right counts as removed")
}

func TestAuthorize_DarwinNeedsAProof(t *testing.T) {
	auth := Default()
	ctx := context.Background()
	cases := []struct {
		name  string
		proof Proof
	}{
		{"no proof", Proof{}},
		{"another kind", Proof{Kind: "polkit", Data: make([]byte, 32)}},
		{"short data", Proof{Kind: proofKind, Data: make([]byte, 8)}},
		{"no live object", Proof{Kind: proofKind, Data: []byte(strings.Repeat("x", 32))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := auth.Authorize(ctx, Request{Action: ActionID, Interactive: true, Proof: tc.proof})
			require.NoError(t, err)
			assert.Equal(t, Result{Challenge: true}, res)
		})
	}
}

func TestHint_Darwin(t *testing.T) {
	assert.Contains(t, Hint(), "desktop session")
}

// A proof of a right with no password reaches the identity check, and the
// check refuses it: nobody authenticated. Only root writes the database.
func TestAuthorize_DarwinChecksWhoAuthenticated(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("writing the authorization database needs root")
	}
	const right = "io.safedep.gryph.test-allow-localauth"
	out, err := exec.Command("security", "authorizationdb", "write", right, "allow").CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { _ = exec.Command("security", "authorizationdb", "remove", right).Run() })

	proof, stop, err := Prepare(context.Background(), right, "Gryph test")
	require.NoError(t, err)
	defer stop()
	require.Equal(t, proofKind, proof.Kind)

	_, err = Default().Authorize(context.Background(), Request{UID: uint32(os.Getuid()), Action: right, Interactive: true, Proof: proof})
	assert.ErrorContains(t, err, "names no user")
}
