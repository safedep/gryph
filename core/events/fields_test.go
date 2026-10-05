package events

import (
	"slices"
	"testing"

	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
)

// TestContentFieldsMatchPayloads keeps privacy.AllFields equal to the
// content fields the payloads and the event hold, so an export rule can
// name every field and no field that no event has.
func TestContentFieldsMatchPayloads(t *testing.T) {
	payloads := []any{
		&FileReadPayload{}, &FileWritePayload{}, &FileDeletePayload{},
		&CommandExecPayload{}, &ToolUsePayload{}, &SessionPayload{},
		&SessionEndPayload{}, &NotificationPayload{}, &SubagentStartPayload{},
		&SubagentStopPayload{}, &UserPromptPayload{}, &TamperPayload{},
	}
	var found []string
	for _, p := range payloads {
		privacy.Walk(p, func(path string, _ *privacy.Text) {
			if !slices.Contains(found, path) {
				found = append(found, path)
			}
		})
	}
	found = append(found, privacy.FieldDiffContent)
	slices.Sort(found)
	want := slices.Clone(privacy.AllFields)
	slices.Sort(want)
	assert.Equal(t, want, found)
}
