package security

import (
	"fmt"
	"strings"
)

// Severity classifies how serious a check decision is. The zero value
// (SeverityUnspecified) means the check did not assign a severity.
type Severity string

const (
	SeverityUnspecified Severity = ""
	SeverityInfo        Severity = "info"
	SeverityLow         Severity = "low"
	SeverityMedium      Severity = "medium"
	SeverityHigh        Severity = "high"
	SeverityCritical    Severity = "critical"
)

// AllSeverities lists every defined severity in ascending order.
var AllSeverities = []Severity{
	SeverityInfo,
	SeverityLow,
	SeverityMedium,
	SeverityHigh,
	SeverityCritical,
}

// ParseSeverity normalizes a string into a Severity. Empty input maps to
// SeverityUnspecified. Unknown values return an error.
func ParseSeverity(s string) (Severity, error) {
	if strings.TrimSpace(s) == "" {
		return SeverityUnspecified, nil
	}
	candidate := Severity(strings.ToLower(strings.TrimSpace(s)))
	for _, sev := range AllSeverities {
		if candidate == sev {
			return sev, nil
		}
	}
	return SeverityUnspecified, fmt.Errorf("invalid severity %q: must be one of %v", s, AllSeverities)
}

// IsValid reports whether the severity is unspecified or one of the known
// constants.
func (s Severity) IsValid() bool {
	if s == SeverityUnspecified {
		return true
	}
	for _, known := range AllSeverities {
		if s == known {
			return true
		}
	}
	return false
}

// Rank returns an ordering value usable for comparison: higher means more
// severe. SeverityUnspecified ranks below all defined severities.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

func (s Severity) String() string {
	return string(s)
}
