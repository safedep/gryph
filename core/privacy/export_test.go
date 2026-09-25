package privacy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportProfile_Apply(t *testing.T) {
	text := func(value string, origin Origin, classes ...Class) Text {
		return Text{Value: value, Label: Label{Origin: origin, Classes: classes, Size: len(value), Digest: Digest(value), Level: "full"}}
	}
	builtin := BuiltinProfiles()
	cases := []struct {
		name       string
		profile    string
		in         Text
		wantValue  string
		wantDigest bool
		wantLabel  bool
		wantTreat  Treatment
	}{
		{"default includes command output", ProfileDefault, text("ok", OriginCommand), "ok", true, true, TreatInclude},
		{"default digests a prompt", ProfileDefault, text("fix it", OriginUser), "", true, true, TreatDigest},
		{"default drops a secret", ProfileDefault, text("k=v", OriginFileProject, ClassSecret), "", false, false, TreatDrop},
		{"default drops pii before the prompt rule", ProfileDefault, text("jane", OriginUser, ClassPII), "", false, false, TreatDrop},
		{"metadata digests everything", ProfileMetadata, text("ok", OriginCommand), "", true, true, TreatDigest},
		{"full includes a secret without its digest", ProfileFull, text("k=v", OriginFileProject, ClassSecret), "k=v", false, true, TreatInclude},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, treatment := builtin[tc.profile].Apply(tc.in)
			assert.Equal(t, tc.wantTreat, treatment)
			assert.Equal(t, tc.wantValue, got.Value)
			assert.Equal(t, tc.wantDigest, got.Label.Digest != "")
			assert.Equal(t, tc.wantLabel, got.Label.Origin != "")
		})
	}

	t.Run("drop keeps the size", func(t *testing.T) {
		p := ExportProfile{Name: "d", Default: TreatDrop}
		got, _ := p.Apply(text("page", OriginWeb))
		assert.Equal(t, Text{Label: Label{Size: 4}}, got)
	})

	t.Run("a pii value loses its digest and size", func(t *testing.T) {
		got, _ := builtin[ProfileMetadata].Apply(text("555-0100", OriginCommand, ClassPII))
		assert.Empty(t, got.Label.Digest)
		assert.Zero(t, got.Label.Size)
	})

	t.Run("redact", func(t *testing.T) {
		p := ExportProfile{Name: "r", Default: TreatRedact}
		got, _ := p.Apply(text("abc", OriginWeb))
		assert.Equal(t, RedactedValue, got.Value)
		assert.Equal(t, OriginWeb, got.Label.Origin)
	})

	t.Run("zero text stays zero", func(t *testing.T) {
		got, treatment := builtin[ProfileMetadata].Apply(Text{})
		assert.True(t, got.IsZero())
		assert.Equal(t, TreatInclude, treatment)
	})
}

func TestExportProfile_ApplyDigest(t *testing.T) {
	digest := Digest("token=abc123 and more")
	cases := []struct {
		name       string
		label      Label
		keepDigest bool
	}{
		{"whole value", Label{Size: 21, Digest: digest}, true},
		{"redacted", Label{Redacted: true, Size: 21, Digest: digest}, false},
		{"secret class", Label{Classes: []Class{ClassSecret}, Size: 21, Digest: digest}, false},
		{"truncated preview", Label{Truncated: true, Size: 21, Digest: digest}, false},
		{"stripped", Label{Stripped: true, Size: 21, Digest: digest}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := BuiltinProfiles()[ProfileFull].Apply(Text{Value: "v", Label: tc.label})
			assert.Equal(t, "v", got.Value)
			if tc.keepDigest {
				assert.Equal(t, digest, got.Label.Digest)
				assert.Equal(t, 21, got.Label.Size)
				return
			}
			assert.Empty(t, got.Label.Digest)
			assert.Zero(t, got.Label.Size)
		})
	}
}

func TestProject(t *testing.T) {
	type payload struct {
		Command Text `json:"command"`
		Output  Text `json:"output"`
	}
	p := &payload{
		Command: Text{Value: "cat .env", Label: Label{Origin: OriginAgent}},
		Output:  Text{Value: "A=1", Label: Label{Origin: OriginCommand, Classes: []Class{ClassSecret}}},
	}
	all := Project(p, BuiltinProfiles()[ProfileDefault])
	assert.False(t, all)
	assert.Equal(t, "cat .env", p.Command.Value)
	assert.Empty(t, p.Output.Value)

	q := &payload{Command: Text{Value: "ls", Label: Label{Origin: OriginAgent}}}
	assert.True(t, Project(q, BuiltinProfiles()[ProfileFull]))
}

func TestExportProfile_Validate(t *testing.T) {
	for name, p := range BuiltinProfiles() {
		require.NoError(t, p.Validate(), name)
	}
	cases := []struct {
		name    string
		profile ExportProfile
		want    string
	}{
		{"bad default", ExportProfile{Name: "x", Default: "keep"}, "unknown default treatment"},
		{"bad then", ExportProfile{Name: "x", Default: TreatInclude, Rules: []ExportRule{{Classes: []Class{ClassPII}, Then: "hide"}}}, "unknown treatment"},
		{"empty rule", ExportProfile{Name: "x", Default: TreatInclude, Rules: []ExportRule{{Then: TreatDrop}}}, "needs classes or origins"},
		{"bad class", ExportProfile{Name: "x", Default: TreatInclude, Rules: []ExportRule{{Classes: []Class{"password"}, Then: TreatDrop}}}, "unknown class"},
		{"bad origin", ExportProfile{Name: "x", Default: TreatInclude, Rules: []ExportRule{{Origins: []Origin{"internet"}, Then: TreatDrop}}}, "unknown origin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.profile.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestExportProfile_IncludesAll(t *testing.T) {
	builtin := BuiltinProfiles()
	assert.True(t, builtin[ProfileFull].IncludesAll())
	assert.False(t, builtin[ProfileDefault].IncludesAll())
	assert.False(t, builtin[ProfileMetadata].IncludesAll())
	assert.True(t, ExportProfile{Default: TreatInclude, Rules: []ExportRule{{Origins: []Origin{OriginWeb}, Then: TreatInclude}}}.IncludesAll())
	assert.False(t, ExportProfile{Default: TreatInclude, Rules: []ExportRule{{Origins: []Origin{OriginWeb}, Then: TreatRedact}}}.IncludesAll())
}

func TestStricter(t *testing.T) {
	assert.Equal(t, TreatDrop, Stricter(TreatDigest, TreatDrop))
	assert.Equal(t, TreatDrop, Stricter(TreatDrop, TreatInclude))
	assert.Equal(t, TreatRedact, Stricter(TreatInclude, TreatRedact))
	assert.Equal(t, TreatInclude, Stricter(TreatInclude, TreatInclude))
}
