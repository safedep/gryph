package mediation

import (
	"github.com/safedep/gryph/aarm/model"
)

// contentMatchMaxBytes caps how many bytes of full content the PDP matches
// against content_patterns. Beyond it, matching runs on the prefix and the
// action is flagged ContentTruncated. Bounds per-event matching cost.
const contentMatchMaxBytes = 1 << 20 // 1 MiB

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
