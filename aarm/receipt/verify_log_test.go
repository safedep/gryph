package receipt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyExportedLogAcceptsCleanChain(t *testing.T) {
	store := storagetest.NewStore(t)
	pkFile, err := GenerateKey("k1")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	pub, err := pkFile.Public()
	require.NoError(t, err)
	signer, err := NewEd25519Signer(priv)
	require.NoError(t, err)

	g := NewSQLite(store, WithSigner(signer))
	ctx := context.Background()
	sessionID := uuid.New()
	for i := 0; i < 4; i++ {
		_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
		require.NoError(t, err)
	}

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:            ExportFormatJSONL,
		IncludeSignatures: true,
		SessionID:         &sessionID,
	}))

	verifier, err := NewEd25519Verifier(&TrustStore{Keys: []TrustStoreEntry{{
		KeyID: pkFile.KeyID,
		Pub:   base64.StdEncoding.EncodeToString(pub),
	}}})
	require.NoError(t, err)

	res, err := VerifyExportedLog(bytes.NewReader(buf.Bytes()), verifier)
	require.NoError(t, err)
	assert.True(t, res.OK, "result: %+v", res)
	assert.Equal(t, 4, res.SignedOK)
	assert.Equal(t, 0, res.SignedInvalid)
	assert.Equal(t, 0, res.ChainBreaks)
}

func TestVerifyExportedLogRejectsTamperedHash(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()
	for i := 0; i < 3; i++ {
		_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
		require.NoError(t, err)
	}
	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:    ExportFormatJSONL,
		SessionID: &sessionID,
	}))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3)

	var second ExportedReceipt
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	second.Message = "TAMPERED"
	raw, err := json.Marshal(&second)
	require.NoError(t, err)
	lines[1] = string(raw)

	combined := strings.Join(lines, "\n") + "\n"
	res, err := VerifyExportedLog(strings.NewReader(combined), nil)
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.GreaterOrEqual(t, res.ChainBreaks, 1)
}

func TestVerifyExportedLogRejectsBrokenSignature(t *testing.T) {
	store := storagetest.NewStore(t)
	pkFile, err := GenerateKey("k1")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	pub, err := pkFile.Public()
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
		Format:            ExportFormatJSONL,
		IncludeSignatures: true,
		SessionID:         &sessionID,
	}))

	var rec ExportedReceipt
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec))
	sigBytes, err := base64.StdEncoding.DecodeString(rec.Signature)
	require.NoError(t, err)
	sigBytes[0] ^= 0xff
	rec.Signature = base64.StdEncoding.EncodeToString(sigBytes)
	tampered, err := json.Marshal(&rec)
	require.NoError(t, err)

	verifier, err := NewEd25519Verifier(&TrustStore{Keys: []TrustStoreEntry{{
		KeyID: pkFile.KeyID,
		Pub:   base64.StdEncoding.EncodeToString(pub),
	}}})
	require.NoError(t, err)

	res, err := VerifyExportedLog(bytes.NewReader(append(tampered, '\n')), verifier)
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Equal(t, 1, res.SignedInvalid)
}

func TestVerifyExportedLogTreatsUnsignedAsOK(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()
	_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:    ExportFormatJSONL,
		SessionID: &sessionID,
	}))

	res, err := VerifyExportedLog(bytes.NewReader(buf.Bytes()), nil)
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, 1, res.Unsigned)
	assert.Equal(t, 0, res.SignedUnverified)
}

func TestVerifyExportedLogTreatsSignedWithoutVerifierAsWarning(t *testing.T) {
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
	for i := 0; i < 3; i++ {
		_, err = g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
		require.NoError(t, err)
	}

	var buf bytes.Buffer
	require.NoError(t, NewSQLiteExporter(store).Export(ctx, &buf, ExportOptions{
		Format:            ExportFormatJSONL,
		IncludeSignatures: true,
		SessionID:         &sessionID,
	}))

	res, err := VerifyExportedLog(bytes.NewReader(buf.Bytes()), nil)
	require.NoError(t, err)
	assert.True(t, res.OK, "signed-no-verifier must not flip OK to false: %+v", res)
	assert.Equal(t, 0, res.SignedInvalid)
	assert.Equal(t, 0, res.ChainBreaks)
	assert.Equal(t, 3, res.SignedUnverified)
	assert.NotEmpty(t, res.Warnings)
	assert.Empty(t, res.Errors)
}
