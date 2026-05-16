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
