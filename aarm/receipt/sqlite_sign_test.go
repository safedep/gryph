package receipt

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pubBase64(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(pub)
}

func TestSQLiteGeneratorSignsReceiptsWhenSignerWired(t *testing.T) {
	store := storagetest.NewStore(t)
	pkFile, err := GenerateKey("test")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	signer, err := NewEd25519Signer(priv)
	require.NoError(t, err)

	g := NewSQLite(store, WithSigner(signer))
	ctx := context.Background()
	sessionID := uuid.New()

	rec, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, pkFile.KeyID, rec.SignerKeyID)

	rows, err := store.QueryReceipts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.NotEmpty(t, rows[0].Signature)
	assert.Equal(t, pkFile.KeyID, rows[0].SignerKeyID)

	pub, err := pkFile.Public()
	require.NoError(t, err)
	require.True(t, len(rows[0].Hash) > 0)
	verifier, err := NewEd25519Verifier(&TrustStore{Keys: []TrustStoreEntry{{
		KeyID: pkFile.KeyID,
		Pub:   pubBase64(t, pub),
	}}})
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(rows[0].Hash, rows[0].Signature, rows[0].SignerKeyID))
}

func TestSQLiteGeneratorLeavesSignatureNilWhenUnsigned(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	rec, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)
	assert.Empty(t, rec.SignerKeyID)

	rows, err := store.QueryReceipts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Signature)
	assert.Empty(t, rows[0].SignerKeyID)
}
