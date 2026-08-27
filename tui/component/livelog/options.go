package livelog

import (
	"time"

	"github.com/safedep/gryph/storage"
)

type Options struct {
	Store        storage.Store
	PollInterval time.Duration
	AgentFilter  string
	// AgentNames is the list of agent names the filter key cycles through.
	// The caller sources it from the adapter registry.
	AgentNames   []string
	InitialLimit int
	Since        time.Time
}

// agentCycle returns the filter cycle: the empty all filter first, then the
// configured agent names.
func (o Options) agentCycle() []string {
	return append([]string{""}, o.AgentNames...)
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
