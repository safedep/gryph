package audit

import (
	"time"
)

// RetentionPolicy defines the data retention settings.
type RetentionPolicy struct {
	// RetentionDays is the number of days to keep events (0 = never delete).
	RetentionDays int
	// KeepSelfAudit indicates whether to exempt self-audit entries from retention.
	KeepSelfAudit bool
}

// NewRetentionPolicy creates a new RetentionPolicy with the given retention days.
func NewRetentionPolicy(days int) *RetentionPolicy {
	return &RetentionPolicy{
		RetentionDays: days,
		KeepSelfAudit: true, // Self-audit entries are never deleted by default
	}
}

// DefaultRetentionPolicy returns the default retention policy (90 days).
func DefaultRetentionPolicy() *RetentionPolicy {
	return NewRetentionPolicy(90)
}

// CutoffTime returns the time before which events should be deleted.
// Returns zero time if retention is disabled (RetentionDays == 0).
func (p *RetentionPolicy) CutoffTime() time.Time {
	if p.RetentionDays == 0 {
		return time.Time{}
	}
	return time.Now().AddDate(0, 0, -p.RetentionDays)
}

// IsEnabled returns true if retention is enabled.
func (p *RetentionPolicy) IsEnabled() bool {
	return p.RetentionDays > 0
}

// ShouldDelete returns true if an event with the given timestamp should be deleted.
func (p *RetentionPolicy) ShouldDelete(eventTime time.Time) bool {
	if !p.IsEnabled() {
		return false
	}
	cutoff := p.CutoffTime()
	return eventTime.Before(cutoff)
}
