package model

import (
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/privacy"
)

// WindowSpec selects a window of the session context. Kinds keeps the
// entries of these kinds. Empty keeps every kind. MaxBytes bounds the total
// bytes of the content values. Labels and entries do not count.
type WindowSpec struct {
	MaxEntries     int
	MaxBytes       int
	Kinds          []EntryKind
	IncludeContent bool
}

// Window is the latest part of the session context, in sequence order. It
// always holds the latest intent that reached the agent. Truncated is true
// when MaxBytes removed content.
type Window struct {
	SessionID uuid.UUID     `json:"session_id"`
	Entries   []WindowEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

// WindowEntry is one entry with its content. A content value holds text
// only when Gryph stored it at the full logging level. Any other value holds
// its label and digest only.
type WindowEntry struct {
	Entry   ContextEntry   `json:"entry"`
	Content []privacy.Text `json:"content,omitempty"`
}
