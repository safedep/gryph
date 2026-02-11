package stats

import (
	"time"

	"github.com/safedep/gryph/storage"
)

type TimeRange int

const (
	RangeToday TimeRange = iota
	Range7Days
	Range30Days
	RangeAll
)

func (r TimeRange) String() string {
	switch r {
	case RangeToday:
		return "Today"
	case Range7Days:
		return "7 Days"
	case Range30Days:
		return "30 Days"
	case RangeAll:
		return "All"
	default:
		return "Unknown"
	}
}

func (r TimeRange) Since() *time.Time {
	now := time.Now()
	var t time.Time
	switch r {
	case RangeToday:
		t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	case Range7Days:
		t = now.UTC().Add(-7 * 24 * time.Hour)
	case Range30Days:
		t = now.UTC().Add(-30 * 24 * time.Hour)
	case RangeAll:
		return nil
	default:
		return nil
	}
	return &t
}

func (r TimeRange) Until() *time.Time {
	return nil
}

type Options struct {
	Store       storage.Store
	TimeRange   TimeRange
	AgentFilter string
	Since       *time.Time
	Until       *time.Time
}
