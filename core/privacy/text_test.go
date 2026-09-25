package privacy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestText_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		data string
		want Text
	}{
		{"object", `{"value":"ls","label":{"level":"full","size":2,"redacted":true}}`,
			Text{Value: "ls", Label: Label{Level: "full", Size: 2, Redacted: true}}},
		{"object without label is legacy json", `{"value":"ls"}`, Text{Value: `{"value":"ls"}`}},
		{"label from a newer version", `{"value":"ls","label":{"level":"full","future":1}}`, Text{Value: "ls", Label: Label{Level: "full"}}},
		{"extra key is legacy json", `{"value":"ls","label":{},"x":1}`, Text{Value: `{"value":"ls","label":{},"x":1}`}},
		{"legacy string", `"npm install"`, Text{Value: "npm install"}},
		{"legacy json object", `{"command": "ls", "timeout": 5}`, Text{Value: `{"command":"ls","timeout":5}`}},
		{"legacy object with a value key", `{"value": 1}`, Text{Value: `{"value":1}`}},
		{"legacy array", `[1, 2]`, Text{Value: `[1,2]`}},
		{"null", `null`, Text{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Text
			require.NoError(t, json.Unmarshal([]byte(tc.data), &got))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestText_JSONRoundTrip(t *testing.T) {
	type payload struct {
		Content Text `json:"content,omitzero"`
		Empty   Text `json:"empty,omitzero"`
	}
	in := payload{Content: Text{Value: "x", Label: Label{Classes: []Class{ClassSecret}, Level: "full", Size: 1, Digest: Digest("x")}}}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"empty"`)

	var out payload
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestPreview(t *testing.T) {
	short := Preview("abc", 10)
	assert.Equal(t, Text{Value: "abc"}, short)

	long := Preview(strings.Repeat("a", 20), 5)
	assert.Equal(t, "aaaaa", long.Value)
	assert.True(t, long.Label.Truncated)
	assert.Equal(t, 20, long.Label.Size)
	assert.Equal(t, Digest(strings.Repeat("a", 20)), long.Label.Digest)

	multi := Preview("a\u00e9\u20acb", 4)
	assert.Equal(t, "a\u00e9", multi.Value)
	assert.True(t, multi.Label.Truncated)
}

func TestLabel_IsZero(t *testing.T) {
	assert.True(t, Label{}.IsZero())
	assert.False(t, Label{Level: "full"}.IsZero())
	assert.False(t, Label{Classes: []Class{ClassPII}}.IsZero())
	assert.True(t, Text{}.IsZero())
	assert.False(t, Text{Label: Label{Stripped: true}}.IsZero())
}

func TestLabel_AddClasses(t *testing.T) {
	var l Label
	l.AddClasses(ClassSecret, ClassConfig, ClassSecret)
	assert.Equal(t, []Class{ClassSecret, ClassConfig}, l.Classes)
}

func TestClass_Valid(t *testing.T) {
	assert.False(t, Class("prompt").Valid())
	assert.True(t, ClassExternalURL.Valid())
}

func TestDigest(t *testing.T) {
	assert.Equal(t, "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", Digest("abc"))
}
