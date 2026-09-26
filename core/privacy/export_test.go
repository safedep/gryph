package privacy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportProfile_Apply(t *testing.T) {
	text := func(value string, origin Origin, classes ...Class) Text {
		return Text{Value: value, Label: Label{Origin: origin, Classes: classes, Size: len(value), Digest: Digest(value), Level: "full"}}
	}
	key := []byte("install-key")
	builtin := BuiltinProfiles()
	builtin["redact"] = ExportProfile{Name: "redact", Default: TreatRedact}
	for name, p := range builtin {
		builtin[name] = p.WithDigestKey(key)
	}
	cases := []struct {
		name       string
		profile    string
		in         Text
		wantValue  string
		wantDigest bool
		wantLabel  bool
		wantTreat  Treatment
	}{
		{"default includes command output without its digest", ProfileDefault, text("ok", OriginCommand), "ok", false, true, TreatInclude},
		{"default digests a prompt", ProfileDefault, text("fix it", OriginUser), "", true, true, TreatDigest},
		{"default drops a secret", ProfileDefault, text("k=v", OriginFileProject, ClassSecret), "", false, false, TreatDrop},
		{"default drops pii before the prompt rule", ProfileDefault, text("jane", OriginUser, ClassPII), "", false, false, TreatDrop},
		{"metadata digests everything", ProfileMetadata, text("ok", OriginCommand), "", true, true, TreatDigest},
		{"full includes a secret without its digest", ProfileFull, text("k=v", OriginFileProject, ClassSecret), "k=v", false, true, TreatInclude},
		{"full includes a value without its digest", ProfileFull, text("ok", OriginCommand), "ok", false, true, TreatInclude},
		{"redact keeps the digest", "redact", text("ok", OriginWeb), RedactedValue, true, true, TreatRedact},
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
		p := ExportProfile{Name: "d", Default: TreatDrop, DigestKey: key}
		got, _ := p.Apply(text("page", OriginWeb))
		assert.Equal(t, Text{Label: Label{Size: 4}}, got)
	})

	t.Run("a pii value loses its digest and size", func(t *testing.T) {
		got, _ := builtin[ProfileMetadata].Apply(text("555-0100", OriginCommand, ClassPII))
		assert.Empty(t, got.Label.Digest)
		assert.Zero(t, got.Label.Size)
	})

	t.Run("an exported digest is keyed", func(t *testing.T) {
		got, _ := builtin[ProfileDefault].Apply(text("yes", OriginUser))
		assert.NotEqual(t, Digest("yes"), got.Label.Digest)
		assert.NotContains(t, got.Label.Digest, Digest("yes")[len("sha256:"):])
		assert.True(t, strings.HasPrefix(got.Label.Digest, KeyedDigestPrefix))
		again, _ := builtin[ProfileDefault].Apply(text("yes", OriginUser))
		assert.Equal(t, got.Label.Digest, again.Label.Digest, "one install matches its own values in one profile and origin")
		other, _ := BuiltinProfiles()[ProfileDefault].WithDigestKey([]byte("other-key")).Apply(text("yes", OriginUser))
		assert.NotEqual(t, got.Label.Digest, other.Label.Digest)
	})

	t.Run("the digest binds the profile and the origin", func(t *testing.T) {
		base, _ := builtin[ProfileMetadata].Apply(text("yes", OriginCommand))
		otherOrigin, _ := builtin[ProfileMetadata].Apply(text("yes", OriginWeb))
		otherProfile, _ := builtin[ProfileDefault].Apply(text("yes", OriginUser))
		asUser, _ := builtin[ProfileMetadata].Apply(text("yes", OriginUser))
		require.NotEmpty(t, base.Label.Digest)
		assert.NotEqual(t, base.Label.Digest, otherOrigin.Label.Digest)
		assert.NotEqual(t, asUser.Label.Digest, otherProfile.Label.Digest)
		renamed := builtin[ProfileMetadata]
		renamed.Name = "archive"
		got, _ := renamed.Apply(text("yes", OriginCommand))
		assert.NotEqual(t, base.Label.Digest, got.Label.Digest)
	})

	t.Run("a digest does not match an included value with the same text", func(t *testing.T) {
		prompt, _ := builtin[ProfileDefault].Apply(text("make deploy", OriginUser))
		command, treatment := builtin[ProfileDefault].Apply(text("make deploy", OriginAgent))
		assert.Equal(t, TreatInclude, treatment)
		assert.Equal(t, "make deploy", command.Value)
		assert.Empty(t, command.Label.Digest)
		assert.NotEmpty(t, prompt.Label.Digest)
		asAgent := builtin[ProfileDefault].KeyedDigest(string(OriginAgent), Digest("make deploy"))
		assert.NotEqual(t, asAgent, prompt.Label.Digest)
	})

	t.Run("no digest leaves without a key", func(t *testing.T) {
		got, _ := BuiltinProfiles()[ProfileDefault].Apply(text("yes", OriginUser))
		assert.Empty(t, got.Label.Digest)
	})

	t.Run("a partial value loses its digest and size", func(t *testing.T) {
		full := strings.Repeat("known build output line\n", 10) + "password=hunter2"
		truncated := Preview(full, 200)
		truncated.Label.Origin = OriginCommand
		require.True(t, truncated.Label.Truncated)
		require.NotContains(t, truncated.Value, "hunter2")
		stripped := text("yes", OriginUser)
		stripped.Value = ""
		stripped.Label.Stripped = true
		for name, in := range map[string]Text{"truncated": truncated, "stripped": stripped} {
			for _, profile := range []string{ProfileDefault, ProfileMetadata, ProfileFull} {
				got, _ := builtin[profile].Apply(in)
				assert.Empty(t, got.Label.Digest, "%s under %s", name, profile)
				assert.Zero(t, got.Label.Size, "%s under %s", name, profile)
			}
		}
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
	redact := ExportProfile{Name: "r", Default: TreatRedact}.WithDigestKey([]byte("key"))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := redact.Apply(Text{Value: "v", Label: tc.label})
			assert.Equal(t, RedactedValue, got.Value)
			if tc.keepDigest {
				assert.Equal(t, redact.KeyedDigest("", digest), got.Label.Digest)
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
