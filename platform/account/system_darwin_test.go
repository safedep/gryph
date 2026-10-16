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
	id, err := freeSystemID("root 0\n_www 70\nx 499\n", "wheel 0\ny 498\n")
	require.NoError(t, err)
	assert.Equal(t, 497, id, "an id a user or a group has is taken, from the top")

	var full strings.Builder
	for i := systemIDFirst; i <= systemIDLast; i++ {
		full.WriteString("u " + strconv.Itoa(i) + "\n")
	}
	_, err = freeSystemID(full.String(), "")
	assert.Error(t, err, "a full range has no free id")
}

// fakeDirectory stands in for dscl. It keeps the records and their
// attributes, and fails the one call in fail.
type fakeDirectory struct {
	records map[string]map[string]string
	calls   [][]string
	fail    string
}

func withDscl(t *testing.T, fail string) *fakeDirectory {
	t.Helper()
	f := &fakeDirectory{records: map[string]map[string]string{}, fail: fail}
	restore := dscl
	dscl = func(_ context.Context, args ...string) (string, error) {
		f.calls = append(f.calls, args)
		if strings.Join(args, " ") == f.fail {
			return "", errors.New("refused")
		}
		switch args[0] {
		case "-list":
			return "root 0\n", nil
		case "-read":
			attrs, ok := f.records[args[1]]
			if !ok {
				return "", errors.New("no such record")
			}
			var out strings.Builder
			for _, key := range args[2:] {
				out.WriteString(key + ": " + attrs[key] + "\n")
			}
			return out.String(), nil
		case "-create":
			if f.records[args[1]] == nil {
				f.records[args[1]] = map[string]string{}
			}
			if len(args) == 4 {
				f.records[args[1]][args[2]] = args[3]
			}
		case "-delete":
			delete(f.records, args[1])
		}
		return "", nil
	}
	t.Cleanup(func() { dscl = restore })
	return f
}

func TestCreateSystem_Darwin(t *testing.T) {
	f := withDscl(t, "")
	require.NoError(t, CreateSystem(context.Background(), "_gryph", "/var/empty"))
	user := f.records["/Users/_gryph"]
	assert.Equal(t, "499", user["UniqueID"])
	assert.Equal(t, "499", user["PrimaryGroupID"])
	assert.Equal(t, "/usr/bin/false", user["UserShell"])
	assert.Equal(t, "1", user["IsHidden"])
	assert.Equal(t, "499", f.records["/Groups/_gryph"]["PrimaryGroupID"])
}

func TestCreateSystem_DarwinRemovesAHalfAccount(t *testing.T) {
	f := withDscl(t, "-create /Users/_gryph UniqueID 499")
	require.Error(t, CreateSystem(context.Background(), "_gryph", "/var/empty"))
	assert.Empty(t, f.records, "both records go")
}

func TestCreateSystem_DarwinRefusesAStaleGroup(t *testing.T) {
	f := withDscl(t, "")
	f.records["/Groups/_gryph"] = map[string]string{"PrimaryGroupID": "301"}
	err := CreateSystem(context.Background(), "_gryph", "/var/empty")
	assert.ErrorContains(t, err, "exists already")
	assert.NotContains(t, f.records, "/Users/_gryph")
	assert.Contains(t, f.records, "/Groups/_gryph", "a record the call did not make stays")
}
