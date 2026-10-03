// Package hookside holds the work the hook process does as the agent user
// before it sends a request to the decision service. It detects the project
// of the working directory and collects the session cost from the agent
// transcript. The service treats both as claims and opens neither path, so
// a service with more privilege than the agent user never reads a file the
// agent named.
package hookside

import (
	"context"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/pricing"
	"github.com/safedep/gryph/utils/projectdetection"
)

// NewRequest builds the decision request for a parsed event, with the
// claims that only the agent user can make.
func NewRequest(ctx context.Context, event *events.Event) *decision.HookRequest {
	req := decision.NewHookRequest(event)
	req.Project = ClaimProject(event.WorkingDirectory)
	if event.ActionType == events.ActionSessionEnd {
		req.Cost = CollectCost(ctx, event.AgentName, event.TranscriptPath, event.SessionID)
	}
	return req
}

// ClaimProject names the project of the working directory: the name a
// project manifest gives, else the directory name.
func ClaimProject(workingDirectory string) decision.ProjectClaim {
	if workingDirectory == "" {
		return decision.ProjectClaim{}
	}
	if info, err := projectdetection.DetectProject(workingDirectory); err == nil && info != nil && info.Name != "" {
		return decision.ProjectClaim{Name: info.Name}
	}
	return decision.ProjectClaim{Name: filepath.Base(workingDirectory)}
}

// CollectCost reads the agent transcript and prices its token usage. It
// returns nil when the agent has no collector, the transcript is missing,
// or the transcript holds no usage.
func CollectCost(ctx context.Context, agentName, transcriptPath string, sessionID uuid.UUID) *cost.SessionCost {
	if transcriptPath == "" {
		return nil
	}

	var collector cost.TokenCollector
	switch agentName {
	case agent.AgentClaudeCode:
		collector = claudecode.NewTranscriptCollector()
	default:
		return nil
	}

	usage, err := collector.Collect(ctx, transcriptPath)
	if err != nil {
		log.Debugf("failed to collect cost data: %v", err)
		return nil
	}
	if usage == nil {
		return nil
	}

	provider, err := pricing.NewBundledProvider()
	if err != nil {
		log.Debugf("failed to create pricing provider: %v", err)
		return nil
	}

	sc, err := cost.NewDefaultCalculator(provider, sessionID, collector.Source()).Calculate(usage)
	if err != nil {
		log.Debugf("failed to calculate cost: %v", err)
		return nil
	}
	return sc
}
