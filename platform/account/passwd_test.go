//go:build !windows

package account

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePasswd(t *testing.T) {
	passwd := `root:x:0:0:root:/root:/bin/bash
# a comment
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
alice:x:1000:1000:Alice:/home/alice:/bin/bash
bob:x:1001:1001::/home/bob:/bin/zsh
gone:x:1002:1002::/home/gone:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
short:x:1003
`
	exists := func(home string) bool { return home != "/home/gone" }
	accounts, err := parsePasswd(strings.NewReader(passwd), 1000, exists)
	require.NoError(t, err)
	assert.Equal(t, []Account{
		{ID: "1000", Name: "alice", Home: "/home/alice"},
		{ID: "1001", Name: "bob", Home: "/home/bob"},
	}, accounts)
}
