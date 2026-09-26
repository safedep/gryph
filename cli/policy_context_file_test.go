package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadContextFile(t *testing.T) {
	write := func(t *testing.T, body string) string {
		p := filepath.Join(t.TempDir(), "ctx.yaml")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}

	t.Run("keys follow the CEL names", func(t *testing.T) {
		s, err := readContextFile(write(t, `
total_actions: 3
session_duration_ms: 1500
tags_seen: [secret_read, audit]
tag_seq: {secret_read: 2}
origins_seen: [web]
entries:
  - {seq: 1, kind: action, action_type: file_read, path: /work/.env, tags: [secret_read]}
`))
		require.NoError(t, err)
		assert.Equal(t, 3, s.TotalActions)
		assert.Equal(t, 1500*time.Millisecond, s.SessionDuration)
		assert.Equal(t, map[string]int64{"secret_read": 2, "audit": 0}, s.TagsSeen)
		assert.Equal(t, []string{"web"}, s.OriginsSeen)
		require.Len(t, s.Entries, 1)
		assert.Equal(t, "/work/.env", s.Entries[0].Path)
	})

	t.Run("empty file", func(t *testing.T) {
		s, err := readContextFile(write(t, ""))
		require.NoError(t, err)
		assert.Zero(t, s.TotalActions)
	})

	for name, body := range map[string]string{
		"unknown key":     "tag_seen: [x]\n",
		"second document": "total_actions: 1\n---\ntotal_actions: 2\n",
		"wrong type":      "tags_seen: {x: 1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readContextFile(write(t, body))
			assert.Error(t, err)
		})
	}
}
