package privacy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// Treatment says what an export does with one content value.
type Treatment string

const (
	// TreatInclude keeps the value and the label.
	TreatInclude Treatment = "include"
	// TreatRedact replaces the value with RedactedValue and keeps the label.
	TreatRedact Treatment = "redact"
	// TreatDigest empties the value and keeps the label, with its keyed
	// digest.
	TreatDigest Treatment = "digest"
	// TreatDrop removes the value and the label, and keeps only the size.
	TreatDrop Treatment = "drop"
)

// AllTreatments lists every treatment, from the one that keeps the most to
// the one that removes the most.
var AllTreatments = []Treatment{TreatInclude, TreatRedact, TreatDigest, TreatDrop}

// RedactedValue replaces a value that the redact treatment removes.
const RedactedValue = "[REDACTED]"

// ExportRule gives a treatment to a value whose label has any of the
// classes or any of the origins, or that sits in any of the fields.
type ExportRule struct {
	Classes []Class   `mapstructure:"classes" json:"classes,omitempty"`
	Origins []Origin  `mapstructure:"origins" json:"origins,omitempty"`
	Fields  []string  `mapstructure:"fields" json:"fields,omitempty"`
	Then    Treatment `mapstructure:"then" json:"then"`
}

// The content fields of an event, as a field rule names them: the JSON
// name of each privacy.Text in a payload, and the diff of the event. A
// test in core/events keeps the list equal to the fields the payloads
// hold, so a rule cannot name a field that no event has.
const (
	FieldCommand              = "command"
	FieldOutput               = "output"
	FieldStdoutPreview        = "stdout_preview"
	FieldStderrPreview        = "stderr_preview"
	FieldInput                = "input"
	FieldOutputPreview        = "output_preview"
	FieldContentPreview       = "content_preview"
	FieldOldString            = "old_string"
	FieldNewString            = "new_string"
	FieldLastAssistantMessage = "last_assistant_message"
	FieldPrompt               = "prompt"
	FieldDiffContent          = "diff_content"
)

// AllFields lists every content field a rule can name.
var AllFields = []string{
	FieldCommand, FieldOutput, FieldStdoutPreview, FieldStderrPreview,
	FieldInput, FieldOutputPreview, FieldContentPreview, FieldOldString,
	FieldNewString, FieldLastAssistantMessage, FieldPrompt, FieldDiffContent,
}

// ContentFields are the fields that hold what an agent read, wrote or
// saw, as opposed to the command, which a rule matches on.
var ContentFields = []string{
	FieldOutput, FieldStdoutPreview, FieldStderrPreview, FieldInput,
	FieldOutputPreview, FieldContentPreview, FieldOldString, FieldNewString,
	FieldLastAssistantMessage, FieldDiffContent,
}

// ExportProfile maps labels to treatments. The first matching rule wins. A
// value that no rule matches gets Default.
type ExportProfile struct {
	Name    string       `json:"name"`
	Default Treatment    `json:"default"`
	Rules   []ExportRule `json:"rules,omitempty"`
	// StripURLs removes the userinfo, the query and the fragment of every
	// URL in a value the export includes. A query often carries a token.
	StripURLs bool `json:"strip_urls,omitempty"`
	// RedactAgain runs the redactor once more, at export time, on every
	// plain value the export includes: the command, the paths, the
	// arguments. A pattern added after the row was stored then applies.
	RedactAgain bool `json:"redact_again,omitempty"`
	// Redact is the redactor that RedactAgain runs. The caller sets it
	// from the configured patterns. Nil runs nothing.
	Redact func(string) string `json:"-"`
	// DigestKey is the secret of the install that keys every exported
	// digest. Without it, no digest leaves the machine.
	DigestKey []byte `json:"-"`
}

// WithRedactor returns the profile with the redactor that RedactAgain runs.
func (p ExportProfile) WithRedactor(redact func(string) string) ExportProfile {
	p.Redact = redact
	return p
}

// PlainValue applies what the profile does to a plain value the export
// includes: the redactor again, and the URL strip. The treatment of the
// value comes first, through Treatment.Plain.
func (p ExportProfile) PlainValue(v string) string {
	if v == "" {
		return v
	}
	if p.RedactAgain && p.Redact != nil {
		v = p.Redact(v)
	}
	if p.StripURLs {
		v = StripURLs(v)
	}
	return v
}

// KeyedDigestPrefix starts every digest that an export holds. It tells a
// reader that the value is not a plain sha256 of the content.
const KeyedDigestPrefix = "hmac-sha256:"

// WithDigestKey returns the profile with the key that keys exported digests.
func (p ExportProfile) WithDigestKey(key []byte) ExportProfile {
	p.DigestKey = key
	return p
}

// KeyedDigest returns "hmac-sha256:<hex>" of a local digest, keyed with the
// digest key. A plain sha256 of a short prompt or password can be reversed
// with a dictionary. The keyed form cannot without the key. The HMAC input
// binds the profile name and the scope, which is the label origin or the
// name of a field. One install matches its own values only within one
// profile and one scope. Thus a digest does not match a value that another
// profile or origin includes. It returns an empty string when the digest is
// empty or the profile has no key.
func (p ExportProfile) KeyedDigest(scope, digest string) string {
	if digest == "" || len(p.DigestKey) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, p.DigestKey)
	mac.Write([]byte(p.Name + "\x00" + scope + "\x00" + digest))
	return KeyedDigestPrefix + hex.EncodeToString(mac.Sum(nil))
}

// Built-in export profile names. A user profile cannot reuse them.
const (
	ProfileDefault  = "default"
	ProfileMetadata = "metadata"
	ProfileFull     = "full"
	ProfilePolicy   = "policy"
)

// BuiltinProfiles returns the built-in export profiles.
//   - default drops secret, pii and unknown_sensitive content, digests user
//     prompts, and includes the rest.
//   - metadata digests every value.
//   - full includes every value.
//   - policy keeps the fields that rules match on, digests prompts and
//     content, drops every secret, pii and unknown_sensitive value, strips
//     the URLs, and runs the redactor again on what it includes.
func BuiltinProfiles() map[string]ExportProfile {
	return map[string]ExportProfile{
		ProfileDefault: {
			Name:    ProfileDefault,
			Default: TreatInclude,
			Rules: []ExportRule{
				{Classes: []Class{ClassSecret, ClassPII, ClassUnknownSensitive}, Then: TreatDrop},
				{Origins: []Origin{OriginUser}, Then: TreatDigest},
			},
		},
		ProfileMetadata: {Name: ProfileMetadata, Default: TreatDigest},
		ProfileFull:     {Name: ProfileFull, Default: TreatInclude},
		ProfilePolicy: {
			Name:    ProfilePolicy,
			Default: TreatInclude,
			Rules: []ExportRule{
				{Classes: []Class{ClassSecret, ClassPII, ClassUnknownSensitive}, Then: TreatDrop},
				{Origins: []Origin{OriginUser}, Then: TreatDigest},
				{Fields: ContentFields, Then: TreatDigest},
			},
			StripURLs:   true,
			RedactAgain: true,
		},
	}
}

// Validate reports an unknown treatment, class or origin.
func (p ExportProfile) Validate() error {
	if !slices.Contains(AllTreatments, p.Default) {
		return fmt.Errorf("export profile %q: unknown default treatment %q", p.Name, p.Default)
	}
	for i, r := range p.Rules {
		if !slices.Contains(AllTreatments, r.Then) {
			return fmt.Errorf("export profile %q rule %d: unknown treatment %q", p.Name, i, r.Then)
		}
		if len(r.Classes) == 0 && len(r.Origins) == 0 && len(r.Fields) == 0 {
			return fmt.Errorf("export profile %q rule %d: a rule needs classes, origins or fields", p.Name, i)
		}
		for _, f := range r.Fields {
			if !slices.Contains(AllFields, f) {
				return fmt.Errorf("export profile %q rule %d: unknown field %q", p.Name, i, f)
			}
		}
		for _, c := range r.Classes {
			if !slices.Contains(AllClasses, c) {
				return fmt.Errorf("export profile %q rule %d: unknown class %q", p.Name, i, c)
			}
		}
		for _, o := range r.Origins {
			if !slices.Contains(AllOrigins, o) {
				return fmt.Errorf("export profile %q rule %d: unknown origin %q", p.Name, i, o)
			}
		}
	}
	return nil
}

// IncludesAll reports whether the profile includes every value as it is.
// A profile that strips URLs or redacts again changes what it includes,
// so the raw event, which holds every value as it was, never leaves under
// it.
func (p ExportProfile) IncludesAll() bool {
	return p.Default == TreatInclude && !p.StripURLs && !p.RedactAgain && !slices.ContainsFunc(p.Rules, func(r ExportRule) bool {
		return r.Then != TreatInclude
	})
}

// Plain applies the treatment to a string that has no label of its own.
// Digest and drop both empty it, because the string has no digest.
func (t Treatment) Plain(v string) string {
	switch {
	case v == "" || t == TreatInclude:
		return v
	case t == TreatRedact:
		return RedactedValue
	default:
		return ""
	}
}

// Stricter returns the treatment that removes more of a value.
func Stricter(a, b Treatment) Treatment {
	if slices.Index(AllTreatments, b) > slices.Index(AllTreatments, a) {
		return b
	}
	return a
}

// Treatment returns the treatment of a label, with no field.
func (p ExportProfile) Treatment(l Label) Treatment {
	return p.TreatmentFor("", l)
}

// TreatmentFor returns the treatment of a value in the field, by its label.
// The field is the dotted JSON path Walk gives, and a field rule matches
// its last segment, so a rule names "command" wherever the command sits.
func (p ExportProfile) TreatmentFor(field string, l Label) Treatment {
	name := field
	if i := strings.LastIndexByte(field, '.'); i >= 0 {
		name = field[i+1:]
	}
	for _, r := range p.Rules {
		if slices.ContainsFunc(r.Classes, l.HasClass) || slices.Contains(r.Origins, l.Origin) || (name != "" && slices.Contains(r.Fields, name)) {
			return r.Then
		}
	}
	return p.Default
}

// DigestExportable reports whether the digest and the size of the value can
// leave the machine. A redacted, truncated or stripped value has a digest of
// content that the export does not show, and that content can hold a secret
// that the redactor did not see. A secret, pii or unknown_sensitive value
// can be low-entropy, such as a short password or a phone number.
func (l Label) DigestExportable() bool {
	return !l.Redacted && !l.Truncated && !l.Stripped &&
		!slices.ContainsFunc([]Class{ClassSecret, ClassPII, ClassUnknownSensitive}, l.HasClass)
}

// Apply returns t with the treatment of the profile, with no field.
func (p ExportProfile) Apply(t Text) (Text, Treatment) {
	return p.ApplyField("", t)
}

// ApplyField returns t with the treatment of the profile for the field. A
// value that the export includes carries no digest, because a reader has
// the value, and gets the plain treatment of the profile: the redactor
// again and the URL strip. A value that the export redacts or digests
// carries the keyed form of KeyedDigest, when DigestExportable allows it.
func (p ExportProfile) ApplyField(field string, t Text) (Text, Treatment) {
	if t.IsZero() {
		return t, TreatInclude
	}
	treatment := p.TreatmentFor(field, t.Label)
	switch {
	case !t.Label.DigestExportable():
		t.Label.Digest = ""
		t.Label.Size = 0
	case treatment == TreatInclude:
		t.Label.Digest = ""
	default:
		t.Label.Digest = p.KeyedDigest(string(t.Label.Origin), t.Label.Digest)
	}
	switch treatment {
	case TreatInclude:
		t.Value = p.PlainValue(t.Value)
	case TreatRedact:
		if t.Value != "" {
			t.Value = RedactedValue
		}
	case TreatDigest:
		t.Value = ""
	case TreatDrop:
		t = Text{Label: Label{Size: t.Label.Size, Unclassified: t.Label.Unclassified}}
	}
	return t, treatment
}

// Project applies the profile to every Text that item holds. item is a
// pointer. It reports whether every value got the include treatment.
func Project(item any, p ExportProfile) bool {
	all := true
	Walk(item, func(path string, t *Text) {
		var treatment Treatment
		*t, treatment = p.ApplyField(path, *t)
		if treatment != TreatInclude {
			all = false
		}
	})
	return all
}
