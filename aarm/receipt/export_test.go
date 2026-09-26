package receipt

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExporterJSONL(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	for i := 0; i < 3; i++ {
		_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
		require.NoError(t, err)
	}

	var buf bytes.Buffer
	exp := NewSQLiteExporter(store)
	require.NoError(t, exp.Export(ctx, &buf, ExportOptions{Format: ExportFormatJSONL}))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, 3)

	var first ExportedReceipt
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.NotEmpty(t, first.Hash)
	assert.Empty(t, first.Signature)
	assert.Empty(t, first.SignerKeyID)
}

func TestExporterJSONLIncludeSignatures(t *testing.T) {
	store := storagetest.NewStore(t)
	pkFile, err := GenerateKey("k1")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	signer, err := NewEd25519Signer(priv)
	require.NoError(t, err)

	g := NewSQLite(store, WithSigner(signer))
	ctx := context.Background()
	sessionID := uuid.New()
	_, err = g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)

	var withSig bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &withSig, ExportOptions{
		Format:            ExportFormatJSONL,
		IncludeSignatures: true,
	}))
	var rec ExportedReceipt
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(withSig.String())), &rec))
	assert.NotEmpty(t, rec.Signature)
	assert.Equal(t, pkFile.KeyID, rec.SignerKeyID)

	var withoutSig bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &withoutSig, ExportOptions{
		Format: ExportFormatJSONL,
	}))
	var rec2 ExportedReceipt
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(withoutSig.String())), &rec2))
	assert.Empty(t, rec2.Signature)
	assert.Empty(t, rec2.SignerKeyID)
}

func TestExporterCSV(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()
	_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{Format: ExportFormatCSV}))

	r := csv.NewReader(&buf)
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "id", records[0][0])
	assert.NotContains(t, records[0], "snapshot")
	assert.NotContains(t, records[0], "action_payload")
	assert.NotContains(t, records[0], "signature")
}

func TestExporterCSVIncludeSignatures(t *testing.T) {
	store := storagetest.NewStore(t)
	pkFile, err := GenerateKey("k1")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	signer, err := NewEd25519Signer(priv)
	require.NoError(t, err)

	g := NewSQLite(store, WithSigner(signer))
	ctx := context.Background()
	sessionID := uuid.New()
	_, err = g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:            ExportFormatCSV,
		IncludeSignatures: true,
	}))
	r := csv.NewReader(&buf)
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Contains(t, records[0], "signature")
	assert.Contains(t, records[0], "signer_key_id")
}

func TestExporterRejectsUnknownFormat(t *testing.T) {
	store := storagetest.NewStore(t)
	exp := NewSQLiteExporter(store)
	err := exp.Export(context.Background(), &bytes.Buffer{}, ExportOptions{Format: "xml"})
	assert.Error(t, err)
}

func TestExporterPagesThroughDuplicateTimestamps(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	const total = 20
	sharedTime := time.Now().UTC().Truncate(time.Microsecond)
	for i := 1; i <= total; i++ {
		row := &storage.ReceiptRow{
			SessionID:    uuid.New(),
			Sequence:     1,
			RecordedAt:   sharedTime,
			ActionType:   "file_read",
			Agent:        "claude-code",
			Tool:         "Read",
			Decision:     "guidance",
			ResultStatus: "success",
			Hash:         []byte(fmt.Sprintf("hash-fixture-%04d-padding-pad", i)),
		}
		require.NoError(t, store.InsertReceipt(ctx, row))
	}

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:    ExportFormatJSONL,
		BatchSize: 5,
	}))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, total)

	seen := map[string]bool{}
	for _, line := range lines {
		var rec ExportedReceipt
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		assert.False(t, seen[rec.ID], "row %s emitted twice", rec.ID)
		seen[rec.ID] = true
	}
	assert.Len(t, seen, total)
}

func TestExporter_ProfileProjectsPayload(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	require.NoError(t, store.SaveSession(ctx, sess))

	record := func(command string, sensitive, withEvent bool) {
		event := events.NewEvent(sess.ID, "claude-code", events.ActionCommandExec)
		event.IsSensitive = sensitive
		require.NoError(t, event.SetPayload(events.CommandExecPayload{
			Command: privacy.Text{Value: command, Label: privacy.Label{Origin: privacy.OriginAgent}},
		}))
		if withEvent {
			require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
		}
		in := newInput(sess.ID, model.DecisionAllow)
		in.EventID = event.ID
		in.Action.Type = model.ActionCommandExec
		in.Action.Parameters = model.Parameters{Command: command}
		in.Decision.Message = "ran " + command
		in.ErrorMessage = "failed: " + command
		_, err := g.Record(ctx, in)
		require.NoError(t, err)
	}
	record("go test ./...", false, true)
	record("cat .env", true, true)
	record("rm -rf build", false, false)

	export := func(profile string) ([]ExportedReceipt, []byte) {
		p := privacy.BuiltinProfiles()[profile]
		var buf bytes.Buffer
		var stats ExportStats
		require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
			Format: ExportFormatJSONL, SessionID: &sess.ID, Profile: &p, Events: store, Stats: &stats,
		}))
		assert.Zero(t, stats.ProjectedV1)
		var rows []ExportedReceipt
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			var r ExportedReceipt
			require.NoError(t, json.Unmarshal([]byte(line), &r))
			rows = append(rows, r)
		}
		require.Len(t, rows, 3)
		return rows, buf.Bytes()
	}
	commands := func(rows []ExportedReceipt) []any {
		out := make([]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.ActionPayload["command"])
		}
		return out
	}

	tests := []struct {
		profile string
		want    []any
	}{
		{privacy.ProfileDefault, []any{"go test ./...", nil, nil}},
		{privacy.ProfileMetadata, []any{nil, nil, nil}},
		{privacy.ProfileFull, []any{"go test ./...", "cat .env", "rm -rf build"}},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			rows, data := export(tt.profile)
			assert.Equal(t, tt.want, commands(rows))
			for _, r := range rows {
				assert.NotEmpty(t, r.CommandDigest, "a profile never changes the digest columns")
				assert.Equal(t, HashV2, r.HashVersion)
				assert.Equal(t, r.ActionPayload["command"] == nil, r.Projected)
				assert.Equal(t, r.Projected, r.ContentSalt == "", "the salt leaves only with the content")
				assert.Equal(t, r.Projected, r.ErrorMessage == "", "the error message gets the same treatment")
			}
			res, err := VerifyExportedLog(bytes.NewReader(data), nil)
			require.NoError(t, err)
			assert.True(t, res.OK, "a v2 export verifies under every profile: %+v", res)
		})
	}
}

func TestVerifyChain_ProjectedV1Row(t *testing.T) {
	fields := HashInputFields{
		Sequence:      1,
		SessionID:     uuid.New(),
		ActionType:    "command_exec",
		Decision:      "allow",
		ActionPayload: map[string]interface{}{"command": "ls"},
	}
	hash, err := ComputeHash(NewHashInput(fields))
	require.NoError(t, err)
	fields.ActionPayload = nil
	breaks := VerifyChain([]ChainRow{{SessionID: fields.SessionID, Sequence: 1, Hash: hash, Fields: fields, Projected: true}})
	require.Len(t, breaks, 1)
	assert.Contains(t, breaks[0].Reason, "Export with --export-profile full")
}

func TestExporter_StatsAndTamper(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	in := newInput(sessionID, model.DecisionBlock)
	in.Action.Type = model.ActionCommandExec
	in.Action.Parameters = model.Parameters{Command: "cat .env"}
	in.Decision.Message = "blocked cat .env"
	_, err := NewSQLite(store).Record(ctx, in)
	require.NoError(t, err)

	recordedAt := time.Now().UTC()
	fields := HashInputFields{
		Sequence: 2, SessionID: sessionID, ActionType: "command_exec", Decision: "allow",
		RecordedAtUnix: recordedAt.UnixNano(), ActionPayload: map[string]interface{}{"command": "ls"},
	}
	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	fields.PrevHash = rows[0].Hash
	hash, err := ComputeHash(NewHashInput(fields))
	require.NoError(t, err)
	require.NoError(t, store.InsertReceipt(ctx, &storage.ReceiptRow{
		SessionID: sessionID, Sequence: 2, PrevHash: fields.PrevHash, Hash: hash, RecordedAt: recordedAt,
		ActionType: "command_exec", Decision: "allow", ActionPayload: fields.ActionPayload,
	}))

	p := privacy.BuiltinProfiles()[privacy.ProfileDefault]
	var buf bytes.Buffer
	var stats ExportStats
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format: ExportFormatJSONL, SessionID: &sessionID, Profile: &p, Events: store, Stats: &stats,
	}))
	assert.Equal(t, 1, stats.ProjectedV1, "a v1 row without its audit event loses its command")
	assert.Equal(t, 1, stats.MessageQuotes)

	res, err := VerifyExportedLog(bytes.NewReader(buf.Bytes()), nil)
	require.NoError(t, err)
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0].Reason, "Export with --export-profile full")

	full := privacy.BuiltinProfiles()[privacy.ProfileFull]
	buf.Reset()
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format: ExportFormatJSONL, SessionID: &sessionID, Profile: &full, Events: store,
	}))
	res, err = VerifyExportedLog(bytes.NewReader(buf.Bytes()), nil)
	require.NoError(t, err)
	assert.True(t, res.OK, "%+v", res)

	tampered := strings.Replace(buf.String(), `"command":"cat .env"`, `"command":"rm -rf /"`, 1)
	res, err = VerifyExportedLog(strings.NewReader(tampered), nil)
	require.NoError(t, err)
	assert.False(t, res.OK)
	require.NotEmpty(t, res.Errors)
	assert.Equal(t, "the command does not match command_digest", res.Errors[0].Reason)
}
