package mediation

import (
	"github.com/safedep/gryph/aarm/model"
)

// contentMatchMaxBytes caps how many bytes of full content the PDP matches
// against content_patterns. Beyond it, matching runs on the prefix and the
// action is flagged ContentTruncated. Bounds per-event matching cost.
const contentMatchMaxBytes = 1 << 20 // 1 MiB

// applyContentMatch sets the ContentFull match buffer of the action from the
// event content, up to contentMatchMaxBytes. It sets ContentTruncated over the
// cap, or when the hook side already cut the content.
func applyContentMatch(action *model.Action, fullContent string, cut bool) {
	if action == nil {
		return
	}
	action.ContentTruncated = cut
	if fullContent == "" {
		return
	}
	if len(fullContent) > contentMatchMaxBytes {
		action.Parameters.ContentFull = fullContent[:contentMatchMaxBytes]
		action.ContentTruncated = true
		return
	}
	action.Parameters.ContentFull = fullContent
}
