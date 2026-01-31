package livelog

import (
	"time"

	"github.com/safedep/gryph/storage"
)

type Options struct {
	Store        storage.Store
	PollInterval time.Duration
	AgentFilter  string
	InitialLimit int
	Since        time.Time
}

func (o Options) pollInterval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return 2 * time.Second
}

func (o Options) initialLimit() int {
	if o.InitialLimit > 0 {
		return o.InitialLimit
	}
	return 50
}
