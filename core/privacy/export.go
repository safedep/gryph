package privacy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
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
// classes or any of the origins.
type ExportRule struct {
	Classes []Class   `mapstructure:"classes" json:"classes,omitempty"`
	Origins []Origin  `mapstructure:"origins" json:"origins,omitempty"`
	Then    Treatment `mapstructure:"then" json:"then"`
}

// ExportProfile maps labels to treatments. The first matching rule wins. A
// value that no rule matches gets Default.
type ExportProfile struct {
	Name    string       `json:"name"`
	Default Treatment    `json:"default"`
	Rules   []ExportRule `json:"rules,omitempty"`
	// DigestKey is the secret of the install that keys every exported
	// digest. Without it, no digest leaves the machine.
	DigestKey []byte `json:"-"`
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
)

// BuiltinProfiles returns the built-in export profiles.
//   - default drops secret, pii and unknown_sensitive content, digests user
//     prompts, and includes the rest.
//   - metadata digests every value.
//   - full includes every value.
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
		if len(r.Classes) == 0 && len(r.Origins) == 0 {
			return fmt.Errorf("export profile %q rule %d: a rule needs classes or origins", p.Name, i)
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

// IncludesAll reports whether the profile includes every value.
func (p ExportProfile) IncludesAll() bool {
	return p.Default == TreatInclude && !slices.ContainsFunc(p.Rules, func(r ExportRule) bool {
		return r.Then != TreatInclude
	})
}

// Stricter returns the treatment that removes more of a value.
func Stricter(a, b Treatment) Treatment {
	if slices.Index(AllTreatments, b) > slices.Index(AllTreatments, a) {
		return b
	}
	return a
}

// Treatment returns the treatment of a label.
func (p ExportProfile) Treatment(l Label) Treatment {
	for _, r := range p.Rules {
		if slices.ContainsFunc(r.Classes, l.HasClass) || slices.Contains(r.Origins, l.Origin) {
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

// Apply returns t with the treatment of the profile. A value that the
// export includes carries no digest, because a reader has the value. A
// value that the export redacts or digests carries the keyed form of
// KeyedDigest, when DigestExportable allows it.
func (p ExportProfile) Apply(t Text) (Text, Treatment) {
	if t.IsZero() {
		return t, TreatInclude
	}
	treatment := p.Treatment(t.Label)
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
	case TreatRedact:
		if t.Value != "" {
			t.Value = RedactedValue
		}
	case TreatDigest:
		t.Value = ""
	case TreatDrop:
		t = Text{Label: Label{Size: t.Label.Size}}
	}
	return t, treatment
}

// Project applies the profile to every Text that item holds. item is a
// pointer. It reports whether every value got the include treatment.
func Project(item any, p ExportProfile) bool {
	all := true
	Walk(item, func(_ string, t *Text) {
		var treatment Treatment
		*t, treatment = p.Apply(*t)
		if treatment != TreatInclude {
			all = false
		}
	})
	return all
}
