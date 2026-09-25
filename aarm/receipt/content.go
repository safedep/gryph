package receipt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/core/privacy"
)

// contentSaltSize is the byte length of the salt of a receipt row.
const contentSaltSize = 16

// contentKeys are the action_payload keys that hold content. Hash v2 reads
// their commitments, not their values.
var contentKeys = []string{payloadKeyCommand, payloadKeyArgs, payloadKeyURL}

func newContentSalt() ([]byte, error) {
	salt := make([]byte, contentSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("receipt: content salt: %w", err)
	}
	return salt, nil
}

// contentDigests returns the commitments of the command, with its args,
// and of the URL in payload. A commitment is sha256 over the row salt and
// the value. The salt leaves the machine only with the values, so a
// commitment of a short secret cannot be reversed by brute force.
func contentDigests(salt []byte, payload map[string]interface{}) (command, url string, err error) {
	if input, ok, err := commandInput(payload); err != nil {
		return "", "", err
	} else if ok {
		command = commit(salt, input)
	}
	if v, ok := payload[payloadKeyURL].(string); ok && v != "" {
		url = commit(salt, []byte(v))
	}
	return command, url, nil
}

// commandInput returns the canonical JSON of the command and the args that
// command_digest commits to.
func commandInput(payload map[string]interface{}) ([]byte, bool, error) {
	group := map[string]interface{}{}
	for _, key := range []string{payloadKeyCommand, payloadKeyArgs} {
		if v, ok := payload[key]; ok {
			group[key] = v
		}
	}
	if len(group) == 0 {
		return nil, false, nil
	}
	data, err := canonical.MarshalJSON(group)
	if err != nil {
		return nil, false, fmt.Errorf("receipt: canonicalize command: %w", err)
	}
	return data, true, nil
}

func commit(salt, input []byte) string {
	h := sha256.New()
	h.Write(salt)
	h.Write(input)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// hasContent reports whether the payload holds a content value that an
// export profile did not remove.
func hasContent(payload map[string]interface{}) bool {
	return slices.ContainsFunc(contentKeys, func(key string) bool {
		v, ok := payload[key]
		return ok && v != privacy.RedactedValue
	})
}

// checkContent returns a reason when a v2 row holds a content value that
// does not match its commitment. A value that an export profile removed or
// redacted is not checked.
func checkContent(salt []byte, f HashInputFields) string {
	if f.HashVersion != HashV2 || !hasContent(f.ActionPayload) {
		return ""
	}
	if len(salt) == 0 {
		return "the action payload holds content without the content salt"
	}
	command, url, err := contentDigests(salt, f.ActionPayload)
	if err != nil {
		return err.Error()
	}
	if cmd, _ := f.ActionPayload[payloadKeyCommand].(string); cmd != privacy.RedactedValue && command != f.CommandDigest {
		return "the command does not match command_digest"
	}
	if u, ok := f.ActionPayload[payloadKeyURL].(string); ok && u != privacy.RedactedValue && url != f.URLDigest {
		return "the URL does not match url_digest"
	}
	return ""
}
