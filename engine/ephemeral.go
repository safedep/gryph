package engine

import (
	"fmt"

	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/security"
)

// NewEphemeralEvaluator builds the evaluator of the local-ephemeral mode:
// the managed policy and the built-in rules, with no store, no
// accumulator, no receipts and no signing, and every rule that reads the
// session context resolving to block. The hook client runs it as the agent
// user in the pilot profile when the decision service is out of reach, so
// it reads the managed policy files, which must be readable by every
// account. The user's own policy never joins: a user file could not
// weaken the policy, but the pilot fallback evaluates what the service
// would, and the service loads the managed sources alone.
func NewEphemeralEvaluator(cfg *config.Config) (*security.Evaluator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("engine: no configuration")
	}
	sec := security.New(&security.Config{FailOpen: false})
	if !cfg.EffectivePolicy().Enabled {
		return sec, nil
	}
	paths := config.ResolvePaths()
	sources := AppendBuiltinSource(managedSources(config.ManagedPolicyState()), cfg, paths)
	med, err := loadPolicyMediator(cfg, paths, nil, loader.New(sources...), aarmsec.WithDegraded())
	if err != nil {
		return nil, err
	}
	sec.RegisterCheck(med)
	return sec, nil
}
