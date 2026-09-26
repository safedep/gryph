// Package privacy holds the content label that travels with every piece of
// agent content Gryph stores, the redactor that removes secrets at write
// time, and the walk that finds labelled content in any value.
package privacy

import "slices"

// Class is a data classification from Gryph's built-in heuristic
// classifier. It is a closed set: a new class is a code change, never a
// config value. The admin's own names for events are rule tags, not classes.
type Class string

const (
	ClassSecret           Class = "secret"
	ClassPII              Class = "pii"
	ClassSourceCode       Class = "source_code"
	ClassConfig           Class = "config"
	ClassGitInternal      Class = "git_internal"
	ClassExternalURL      Class = "external_url"
	ClassUnknownSensitive Class = "unknown_sensitive"
)

// AllClasses lists every class.
var AllClasses = []Class{
	ClassSecret, ClassPII, ClassSourceCode, ClassConfig, ClassGitInternal,
	ClassExternalURL, ClassUnknownSensitive,
}

// Valid reports whether c is a known class.
func (c Class) Valid() bool {
	return slices.Contains(AllClasses, c)
}

// Strings returns the classes as strings, for CEL and storage.
func Strings(classes []Class) []string {
	if classes == nil {
		return nil
	}
	out := make([]string, len(classes))
	for i, c := range classes {
		out[i] = string(c)
	}
	return out
}
