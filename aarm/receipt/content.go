package receipt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"

	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/core/privacy"
)

// contentSaltSize is the byte length of the salt of a receipt row.
const contentSaltSize = 16

// contentDomain tags each commitment, so a commitment of one kind never
// equals a commitment of another kind or of another protocol.
const contentDomain = "gryph.receipt.content.v2"

const (
	commitKindCommand = "command"
	commitKindURL     = "url"
)

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
// and of the URL in payload. A commitment is sha256 over the domain, the
// kind, the row salt and the value, each with a length prefix. The salt
// leaves the machine only with the values, so a commitment of a short
// secret cannot be reversed by brute force.
func contentDigests(salt []byte, payload map[string]interface{}) (command, url string, err error) {
	if input, ok, err := commandInput(payload); err != nil {
		return "", "", err
	} else if ok {
		command = commit(commitKindCommand, salt, input)
	}
	if v, ok := payload[payloadKeyURL].(string); ok && v != "" {
		url = commit(commitKindURL, salt, []byte(v))
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

// commit puts a length prefix on each field. Without it, bytes can move
// from the value into the salt and the commitment still matches.
func commit(kind string, salt, input []byte) string {
	h := sha256.New()
	for _, field := range [][]byte{[]byte(contentDomain), []byte(kind), salt, input} {
		writeField(h, field)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func writeField(h hash.Hash, field []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(field)))
	h.Write(n[:])
	h.Write(field)
}

// hasContent reports whether the payload holds a content value that an
// export profile did not remove.
func hasContent(payload map[string]interface{}) bool {
	return slices.ContainsFunc(contentKeys, func(key string) bool {
		v, ok := payload[key]
		return ok && v != privacy.RedactedValue
	})
}

// checkContent returns a reason when the content of a v2 row does not match
// its commitments. A row with the salt must match exactly. A row without the
// salt may hold only redacted markers. It may have commitments only when an
// export profile projected it.
func checkContent(r ChainRow) string {
	f := r.Fields
	if f.HashVersion != HashV2 {
		return ""
	}
	if len(r.ContentSalt) == 0 {
		if hasContent(f.ActionPayload) {
			return "the action payload holds content without the content salt"
		}
		if !r.Projected && (f.CommandDigest != "" || f.URLDigest != "") {
			return "the content that the commitments cover is missing"
		}
		return ""
	}
	if len(r.ContentSalt) != contentSaltSize {
		return fmt.Sprintf("the content salt has %d bytes, expected %d", len(r.ContentSalt), contentSaltSize)
	}
	if v, ok := f.ActionPayload[payloadKeyURL]; ok {
		if _, isString := v.(string); !isString {
			return "the URL is not a string"
		}
	}
	command, url, err := contentDigests(r.ContentSalt, f.ActionPayload)
	if err != nil {
		return err.Error()
	}
	if command != f.CommandDigest {
		return "the command does not match command_digest"
	}
	if url != f.URLDigest {
		return "the URL does not match url_digest"
	}
	return ""
}
