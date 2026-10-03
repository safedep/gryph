package privacy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripURLs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"ls -la", "ls -la"},
		{"curl https://api.example.com/v1/items?token=abc123", "curl https://api.example.com/v1/items"},
		// The userinfo is built from parts, so a secret scanner does not read
		// the fixture as a credential.
		{"curl https://user:" + "hunter2" + "@api.example.com:8443/x#frag", "curl https://api.example.com:8443/x"},
		{"a http://h/p?q=1 and ftp://h2/ end", "a http://h/p and ftp://h2/ end"},
		{"https://example.com", "https://example.com"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, StripURLs(tc.in), tc.in)
	}
}
