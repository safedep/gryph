package mediation

import (
	"strings"

	"github.com/safedep/gryph/aarm/model"
)

// contentMatchMaxBytes caps how many bytes of full content the PDP matches
// against content_patterns. Beyond it, matching runs on the prefix and the
// action is flagged ContentTruncated. Bounds per-event matching cost.
const contentMatchMaxBytes = 1 << 20 // 1 MiB

// phaseOverrides maps lowercased hook identifiers the prefix/suffix heuristic
// would misclassify to an explicit phase.
var phaseOverrides = map[string]model.ActionPhase{
	"posttooluse":         model.PhasePost,
	"posttoolusefailure":  model.PhasePost,
	"tool.execute.before": model.PhasePre,
	"tool.execute.after":  model.PhasePost,
	// pi agent: tool_call intercepts before execution, tool_result after.
	"tool_call":   model.PhasePre,
	"tool_result": model.PhasePost,
}

// phaseForHookType classifies a raw agent hook identifier as pre- or
// post-execution via an override table then a prefix/suffix heuristic.
// Unrecognized identifiers map to PhaseUnknown, the safe default.
func phaseForHookType(hookType string) model.ActionPhase {
	h := strings.ToLower(strings.TrimSpace(hookType))
	if h == "" {
		return model.PhaseUnknown
	}
	if p, ok := phaseOverrides[h]; ok {
		return p
	}
	switch {
	case strings.HasPrefix(h, "pre"), strings.HasPrefix(h, "before"):
		return model.PhasePre
	case strings.HasPrefix(h, "post"), strings.HasPrefix(h, "after"):
		return model.PhasePost
	case strings.HasSuffix(h, "before"):
		return model.PhasePre
	case strings.HasSuffix(h, "after"):
		return model.PhasePost
	default:
		return model.PhaseUnknown
	}
}

// applyContentMatch sets the action's ContentFull match buffer from the event
// content, capped at contentMatchMaxBytes; over the cap it sets ContentTruncated.
func applyContentMatch(action *model.Action, fullContent string) {
	if action == nil || fullContent == "" {
		return
	}
	if len(fullContent) > contentMatchMaxBytes {
		action.Parameters.ContentFull = fullContent[:contentMatchMaxBytes]
		action.ContentTruncated = true
		return
	}
	action.Parameters.ContentFull = fullContent
}
