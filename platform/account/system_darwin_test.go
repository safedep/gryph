package account

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreeSystemID(t *testing.T) {
	id, err := freeSystemID("root 0\n_www 70\nx 300\n", "wheel 0\ny 301\n")
	require.NoError(t, err)
	assert.Equal(t, 302, id, "an id a user or a group has is taken")

	var full strings.Builder
	for i := systemIDFirst; i <= systemIDLast; i++ {
		full.WriteString("u " + strconv.Itoa(i) + "\n")
	}
	_, err = freeSystemID(full.String(), "")
	assert.Error(t, err, "a full range has no free id")
}

func withDscl(t *testing.T, fail string) *[][]string {
	t.Helper()
	var calls [][]string
	restore := dscl
	dscl = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		if strings.Join(args, " ") == fail {
			return "", errors.New("refused")
		}
		if args[0] == "-list" {
			return "root 0\n", nil
		}
		return "", nil
	}
	t.Cleanup(func() { dscl = restore })
	return &calls
}

func TestCreateSystem_Darwin(t *testing.T) {
	calls := withDscl(t, "")
	require.NoError(t, CreateSystem(context.Background(), "_gryph", "/var/empty"))
	assert.Contains(t, *calls, []string{"-create", "/Users/_gryph", "UniqueID", "300"})
	assert.Contains(t, *calls, []string{"-create", "/Groups/_gryph", "PrimaryGroupID", "300"})
	assert.Contains(t, *calls, []string{"-create", "/Users/_gryph", "UserShell", "/usr/bin/false"})
	assert.Contains(t, *calls, []string{"-create", "/Users/_gryph", "IsHidden", "1"})
}

func TestCreateSystem_DarwinRemovesAHalfAccount(t *testing.T) {
	calls := withDscl(t, "-create /Users/_gryph UniqueID 300")
	require.Error(t, CreateSystem(context.Background(), "_gryph", "/var/empty"))
	assert.Contains(t, *calls, []string{"-delete", "/Users/_gryph"})
	assert.Contains(t, *calls, []string{"-delete", "/Groups/_gryph"})
}
