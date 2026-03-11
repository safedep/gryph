package cli

import (
	"context"
	"encoding/json"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/pricing"
	"github.com/safedep/gryph/storage"
)

func recoverTranscriptPath(ctx context.Context, store storage.Store, sess *session.Session) {
	if sess.TranscriptPath != "" {
		return
	}

	evts, err := store.GetEventsBySession(ctx, sess.ID)
	if err != nil {
		log.Errorf("failed to get events for session %s: %v", sess.ID, err)
		return
	}
	if len(evts) == 0 {
		return
	}

	for _, evt := range evts {
		if len(evt.RawEvent) == 0 {
			continue
		}
		var raw struct {
			TranscriptPath string `json:"transcript_path"`
		}
		if err := json.Unmarshal(evt.RawEvent, &raw); err != nil {
			continue
		}
		if raw.TranscriptPath != "" {
			sess.TranscriptPath = raw.TranscriptPath
			if err := store.UpdateSession(ctx, sess); err != nil {
				log.Errorf("failed to update session transcript path: %v", err)
			}
			return
		}
	}
}

func collectSessionCost(sess *session.Session) {
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
