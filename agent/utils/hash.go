package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashContent returns the lowercase hex SHA-256 hash of the given content.
// Returns an empty string for empty input.
func HashContent(content string) string {
	if content == "" {
		return ""
	}
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
