package pdp

import (
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
)

func TestShellLine(t *testing.T) {
	cases := []struct {
		name   string
		params model.Parameters
		want   string
	}{
		{"command only", model.Parameters{Command: "rm -rf ~/.cc"}, "rm -rf ~/.cc"},
		{"command and args", model.Parameters{Command: "rm", Args: []string{"-rf", "a b"}}, "rm -rf 'a b'"},
		{"args only", model.Parameters{Args: []string{"rm", "x"}}, "rm x"},
		{"empty", model.Parameters{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shellLine(tc.params))
		})
	}
}
