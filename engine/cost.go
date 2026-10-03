package engine

import (
	"context"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/pricing"
)

func CollectSessionCost(sess *session.Session) {
	if sess.TranscriptPath == "" {
		return
	}

	var collector cost.TokenCollector
	switch sess.AgentName {
	case agent.AgentClaudeCode:
		collector = claudecode.NewTranscriptCollector()
	default:
		return
	}

	usage, err := collector.Collect(context.Background(), sess.TranscriptPath)
	if err != nil {
		log.Debugf("failed to collect cost data: %v", err)
		return
	}
	if usage == nil {
		return
	}

	provider, err := pricing.NewBundledProvider()
	if err != nil {
		log.Debugf("failed to create pricing provider: %v", err)
		return
	}

	calc := cost.NewDefaultCalculator(provider, sess.ID, collector.Source())
	sc, err := calc.Calculate(usage)
	if err != nil {
		log.Debugf("failed to calculate cost: %v", err)
		return
	}
	if sc == nil {
		return
	}

	sess.InputTokens = sc.Usage.InputTokens
	sess.OutputTokens = sc.Usage.OutputTokens
	sess.CacheReadTokens = sc.Usage.CacheReadTokens
	sess.CacheWriteTokens = sc.Usage.CacheWriteTokens
	sess.EstimatedCostUSD = sc.TotalCost
	sess.ModelUsage = sc.Usage.Models
	sess.CostSource = string(sc.Source)
	now := sc.ComputedAt
	sess.CostComputedAt = &now
}
