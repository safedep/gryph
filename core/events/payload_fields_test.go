package events

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPayloadStringFields lists the plain string fields of each payload
// type. Agent content must be a privacy.Text so it carries a label. A new
// string field fails this test. Add it here only when it holds an
// identifier, such as a path, a URL, a tool name, or a session ID.
func TestPayloadStringFields(t *testing.T) {
	allowed := map[string][]string{
		"FileReadPayload":      {"Path", "Pattern", "ContentHash"},
		"FileWritePayload":     {"Path", "ContentHash"},
		"FileDeletePayload":    {"Path"},
		"CommandExecPayload":   {"Description"},
		"ToolUsePayload":       {"ToolName"},
		"SessionPayload":       {"Source", "Model", "AgentType"},
		"SessionEndPayload":    {"Reason"},
		"NotificationPayload":  {"Message", "Type"},
		"SubagentStartPayload": {"AgentID", "AgentType"},
		"SubagentStopPayload":  {"AgentID", "AgentType", "AgentTranscriptPath"},
		"UserPromptPayload":    nil,
	}
	for _, at := range []ActionType{
		ActionFileRead, ActionFileWrite, ActionFileDelete, ActionCommandExec, ActionToolUse,
		ActionSessionStart, ActionSessionEnd, ActionNotification, ActionSubagentStart, ActionSubagentStop,
		ActionUserPrompt,
	} {
		typ := reflect.TypeOf(NewPayload(at)).Elem()
		t.Run(typ.Name(), func(t *testing.T) {
			var got []string
			for i := range typ.NumField() {
				if typ.Field(i).Type.Kind() == reflect.String {
					got = append(got, typ.Field(i).Name)
				}
			}
			assert.ElementsMatch(t, allowed[typ.Name()], got)
		})
	}
}
