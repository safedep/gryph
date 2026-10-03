package supervisor

import (
	"sync"
	"time"
)

// Limits bound what one account can ask of the service. A same-user
// process can flood its own account; the limits keep that flood from
// touching another account's service.
type Limits struct {
	// MaxConns is the count of open connections per account. The newest
	// connection above it gets Error rate_limited and closes.
	MaxConns int
	// Rate is the count of requests per second per account that the token
	// bucket refills, and Burst the size of the bucket.
	Rate  float64
	Burst int
	// IdleTimeout ends a connection that sends nothing for this long.
	IdleTimeout time.Duration
}

// DefaultLimits are the limits of a service without configuration.
func DefaultLimits() Limits {
	return Limits{MaxConns: 16, Rate: 20, Burst: 40, IdleTimeout: 30 * time.Second}
}

// bucket is a token bucket for one account.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func newBucket(rate float64, burst int, now time.Time) *bucket {
	return &bucket{tokens: float64(burst), last: now, rate: rate, burst: float64(burst)}
}

// take spends one token. It reports false when the bucket is empty.
func (b *bucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
