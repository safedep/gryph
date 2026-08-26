package security

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
