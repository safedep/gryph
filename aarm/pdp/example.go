package pdp

import _ "embed"

//go:embed policy.example.yml
var exampleYAML string

// ExampleYAML returns the canonical, fully-commented example policy. It is
// the source consumed by `gryph policy init` and is verified to parse cleanly
// in the pdp test suite.
func ExampleYAML() string {
	return exampleYAML
}
