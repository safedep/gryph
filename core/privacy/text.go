package privacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"unicode/utf8"
)

// Origin is where content came from. The adapter claims it from facts it
// knows. Gryph does not decide which origins are trusted. A policy does.
type Origin string

const (
	OriginUser         Origin = "user"
	OriginAgent        Origin = "agent"
	OriginFileProject  Origin = "file_project"
	OriginFileExternal Origin = "file_external"
	OriginCommand      Origin = "command"
	OriginWeb          Origin = "web"
	OriginMCP          Origin = "mcp"
	OriginUnknown      Origin = "unknown"
)

// Label is the metadata of one piece of stored content. Exporters read it
// to decide what leaves the machine.
type Label struct {
	Classes []Class `json:"classes,omitempty"`
	Origin  Origin  `json:"origin,omitempty"`
	// Source names the MCP server when Origin is OriginMCP.
	Source string `json:"source,omitempty"`
	// Redacted is true when a redaction pattern changed the value.
	Redacted bool `json:"redacted,omitempty"`
	// Truncated is true when the value holds only a prefix of the content.
	Truncated bool `json:"truncated,omitempty"`
	// Stripped is true when the logging level removed the value.
	Stripped bool `json:"stripped,omitempty"`
	// Level is the logging level in force when Gryph stored the value.
	// Readers use it, not the current config.
	Level string `json:"level,omitempty"`
	// Size is the byte length of the content before redaction and
	// truncation.
	Size int `json:"size,omitempty"`
	// Digest is "sha256:<hex>" of the content before redaction and
	// truncation.
	Digest string `json:"digest,omitempty"`
}

// IsZero reports whether the label holds no data.
func (l Label) IsZero() bool {
	rest := l
	rest.Classes = nil
	return len(l.Classes) == 0 && reflect.ValueOf(rest).IsZero()
}

// HasClass reports whether the label has class c.
func (l Label) HasClass(c Class) bool {
	return slices.Contains(l.Classes, c)
}

// AddClasses adds each class the label does not have yet.
func (l *Label) AddClasses(classes ...Class) {
	for _, c := range classes {
		if !l.HasClass(c) {
			l.Classes = append(l.Classes, c)
		}
	}
}

// Text is agent content with its label. Payload content fields use it, so
// every stored value carries its privacy facts.
type Text struct {
	Value string `json:"value"`
	Label Label  `json:"label"`
}

// NewText returns a Text with an empty label.
func NewText(v string) Text {
	return Text{Value: v}
}

// Preview returns a Text that holds at most maxBytes bytes of v, cut at a
// character boundary. The label
// records the size and the digest of all of v, so a reader can tell that two
// previews come from the same content.
func Preview(v string, maxBytes int) Text {
	t := Text{Value: v}
	if maxBytes <= 0 || len(v) <= maxBytes {
		return t
	}
	t.Label.Size = len(v)
	t.Label.Digest = Digest(v)
	t.Label.Truncated = true
	for maxBytes > 0 && !utf8.RuneStart(v[maxBytes]) {
		maxBytes--
	}
	t.Value = v[:maxBytes]
	return t
}

// String returns the value.
func (t Text) String() string {
	return t.Value
}

// IsZero reports whether the text holds no value and no label. JSON output
// omits such a field.
func (t Text) IsZero() bool {
	return t.Value == "" && t.Label.IsZero()
}

// UnmarshalJSON reads a Text object. It also reads the forms that rows from
// before content labels hold: a bare JSON string, or any other JSON value,
// which becomes the compact JSON text of that value.
//
// An object is a Text only when its keys are exactly "value" and "label" and
// the value is a string. A writer always writes both keys, and an old tool
// input with exactly these two keys is very unlikely. The label decodes
// without a strict field check, so a label field that a newer version adds
// does not turn the Text into raw JSON.
func (t *Text) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) == 0 || bytes.Equal(data, []byte("null")):
		*t = Text{}
		return nil
	case data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*t = Text{Value: s}
		return nil
	case data[0] == '{':
		if text, ok := decodeTextObject(data); ok {
			*t = text
			return nil
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return err
	}
	*t = Text{Value: compact.String()}
	return nil
}

func decodeTextObject(data []byte) (Text, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || len(obj) != 2 {
		return Text{}, false
	}
	rawValue, hasValue := obj["value"]
	rawLabel, hasLabel := obj["label"]
	if !hasValue || !hasLabel {
		return Text{}, false
	}
	var t Text
	if err := json.Unmarshal(rawValue, &t.Value); err != nil {
		return Text{}, false
	}
	if err := json.Unmarshal(rawLabel, &t.Label); err != nil {
		return Text{}, false
	}
	return t, true
}

// Digest returns "sha256:<hex>" of v.
func Digest(v string) string {
	sum := sha256.Sum256([]byte(v))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ForExport returns t without the digest and the size when the value was
// redacted or has the secret class. A digest of a low-entropy secret, such as
// a short password, can be reversed by brute force, so it must not leave the
// machine. Export profiles replace this rule.
func (t Text) ForExport() Text {
	if t.Label.Redacted || t.Label.HasClass(ClassSecret) {
		t.Label.Digest = ""
		t.Label.Size = 0
	}
	return t
}
