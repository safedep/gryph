package events

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvent_ForExport(t *testing.T) {
	newEvent := func() *Event {
		e := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
		require.NoError(t, e.SetPayload(CommandExecPayload{
			Command: privacy.Text{Value: "cat notes.txt", Label: privacy.Label{Origin: privacy.OriginAgent}},
			Output:  privacy.Text{Value: "notes", Label: privacy.Label{Origin: privacy.OriginCommand, Digest: privacy.Digest("notes")}},
		}))
		e.RawEvent = json.RawMessage(`{"tool_response":"notes"}`)
		return e
	}
	profiles := privacy.BuiltinProfiles()

	t.Run("full keeps the raw event", func(t *testing.T) {
		out := newEvent().ForExport(profiles[privacy.ProfileFull])
		assert.NotNil(t, out.RawEvent)
		p, err := out.GetCommandExecPayload()
		require.NoError(t, err)
		assert.Equal(t, "notes", p.Output.Value)
	})

	t.Run("metadata drops the raw event and every value", func(t *testing.T) {
		in := newEvent()
		out := in.ForExport(profiles[privacy.ProfileMetadata])
		assert.Nil(t, out.RawEvent, "the raw event holds values that the profile removes")
		p, err := out.GetCommandExecPayload()
		require.NoError(t, err)
		assert.Empty(t, p.Command.Value)
		assert.Empty(t, p.Output.Value)
		assert.Equal(t, privacy.Digest("notes"), p.Output.Label.Digest)
		assert.NotNil(t, in.RawEvent, "the source event does not change")
	})

	t.Run("an untyped payload leaves only with a profile that includes all", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionUnknown)
		e.Payload = json.RawMessage(`{"text":"anything"}`)
		assert.Nil(t, e.ForExport(profiles[privacy.ProfileDefault]).Payload)
		assert.NotNil(t, e.ForExport(profiles[privacy.ProfileFull]).Payload)
	})
}

func TestEvent_ForExportPlainFields(t *testing.T) {
	profiles := privacy.BuiltinProfiles()
	sensitiveCommand := func() *Event {
		e := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
		e.IsSensitive = true
		e.ErrorMessage = "DB_PASSWORD=hunter2"
		require.NoError(t, e.SetPayload(CommandExecPayload{
			Command:     privacy.Text{Value: "cat .env", Label: privacy.Label{Origin: privacy.OriginAgent, Classes: []privacy.Class{privacy.ClassSecret}}},
			Description: "read the env file",
			Args:        []string{".env"},
		}))
		return e
	}

	t.Run("a secret event drops its plain fields under default", func(t *testing.T) {
		out := sensitiveCommand().ForExport(profiles[privacy.ProfileDefault])
		assert.Empty(t, out.ErrorMessage)
		p, err := out.GetCommandExecPayload()
		require.NoError(t, err)
		assert.Empty(t, p.Description)
		assert.Equal(t, []string{""}, p.Args)
	})

	t.Run("full keeps the plain fields", func(t *testing.T) {
		out := sensitiveCommand().ForExport(profiles[privacy.ProfileFull])
		assert.Equal(t, "DB_PASSWORD=hunter2", out.ErrorMessage)
		p, err := out.GetCommandExecPayload()
		require.NoError(t, err)
		assert.Equal(t, "read the env file", p.Description)
	})

	t.Run("metadata empties a payload without labels", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionNotification)
		require.NoError(t, e.SetPayload(NotificationPayload{Message: "jane@example.com", Details: json.RawMessage(`{"a":1}`)}))
		e.RawEvent = json.RawMessage(`{"message":"jane@example.com"}`)
		out := e.ForExport(profiles[privacy.ProfileMetadata])
		assert.Nil(t, out.RawEvent)
		assert.NotContains(t, string(out.Payload), "jane@example.com")
		assert.NotContains(t, string(out.Payload), "details")
	})

	t.Run("a file read keeps no raw event under default", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionFileRead)
		require.NoError(t, e.SetPayload(FileReadPayload{Path: "/work/a.txt"}))
		e.RawEvent = json.RawMessage(`{"tool_response":{"file":{"content":"READ_MARKER"}}}`)
		assert.Nil(t, e.ForExport(profiles[privacy.ProfileDefault]).RawEvent)
	})
}

func TestEvent_ForExportLegacyRows(t *testing.T) {
	profiles := privacy.BuiltinProfiles()

	t.Run("a sensitive row without labels drops its content", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
		e.IsSensitive = true
		e.Payload = json.RawMessage(`{"command":"cat ~/.aws/credentials","output":"aws_secret_access_key=abc"}`)
		out := e.ForExport(profiles[privacy.ProfileDefault])
		assert.NotContains(t, string(out.Payload), "aws_secret_access_key")
		assert.NotContains(t, string(out.Payload), "credentials")
	})

	t.Run("a prompt without an origin gets the origin user", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionUserPrompt)
		e.Payload = json.RawMessage(`{"prompt":"my ssn is 078-05-1120"}`)
		out := e.ForExport(profiles[privacy.ProfileDefault])
		assert.NotContains(t, string(out.Payload), "078-05-1120")
		assert.Contains(t, string(out.Payload), `"origin":"user"`)
	})

	t.Run("a payload that does not decode stays only under full", func(t *testing.T) {
		e := NewEvent(uuid.New(), "claude-code", ActionCommandExec)
		e.Payload = json.RawMessage(`{"exit_code":"x"}`)
		assert.Nil(t, e.ForExport(profiles[privacy.ProfileDefault]).Payload)
		assert.JSONEq(t, `{"exit_code":"x"}`, string(e.ForExport(profiles[privacy.ProfileFull]).Payload))
	})
}
