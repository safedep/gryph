package receipt

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustGenSigner(t *testing.T) (*Ed25519Signer, *Ed25519Verifier, ed25519.PublicKey) {
	t.Helper()
	pkFile, err := GenerateKey("test")
	require.NoError(t, err)
	priv, err := pkFile.PrivateKey()
	require.NoError(t, err)
	pub, err := pkFile.Public()
	require.NoError(t, err)
	signer, err := NewEd25519Signer(priv)
	require.NoError(t, err)
	ts := &TrustStore{Keys: []TrustStoreEntry{{
		KeyID:   pkFile.KeyID,
		Pub:     base64.StdEncoding.EncodeToString(pub),
		Created: time.Now().UTC(),
	}}}
	verifier, err := NewEd25519Verifier(ts)
	require.NoError(t, err)
	return signer, verifier, pub
}

func TestEd25519SignVerifyRoundTrip(t *testing.T) {
	signer, verifier, _ := mustGenSigner(t)
	hash := []byte("hash-fixture-32-bytes-aaaaaaaaaa")
	sig, keyID, err := signer.Sign(hash)
	require.NoError(t, err)
	assert.Len(t, sig, ed25519.SignatureSize)
	assert.Len(t, keyID, KeyIDLen)
	assert.True(t, verifier.HasKey(keyID))
	require.NoError(t, verifier.Verify(hash, sig, keyID))
}

func TestEd25519VerifierRejectsBadSignature(t *testing.T) {
	signer, verifier, _ := mustGenSigner(t)
	hash := []byte("hash-fixture-32-bytes-aaaaaaaaaa")
	sig, keyID, err := signer.Sign(hash)
	require.NoError(t, err)
	sig[0] ^= 0xff
	err = verifier.Verify(hash, sig, keyID)
	assert.Error(t, err)
}

func TestEd25519VerifierRejectsUnknownKeyID(t *testing.T) {
	_, verifier, _ := mustGenSigner(t)
	hash := []byte("hash-fixture-32-bytes-aaaaaaaaaa")
	sig := make([]byte, ed25519.SignatureSize)
	err := verifier.Verify(hash, sig, "0123456789abcdef")
	assert.Error(t, err)
	assert.False(t, verifier.HasKey("0123456789abcdef"))
}

func TestEd25519VerifierRejectsCrossKey(t *testing.T) {
	signerA, _, _ := mustGenSigner(t)
	_, verifierB, _ := mustGenSigner(t)
	hash := []byte("hash-fixture-32-bytes-aaaaaaaaaa")
	sig, keyID, err := signerA.Sign(hash)
	require.NoError(t, err)
	err = verifierB.Verify(hash, sig, keyID)
	assert.Error(t, err)
}

func TestPrivateKeyFileRoundTrip(t *testing.T) {
	pk, err := GenerateKey("local-host")
	require.NoError(t, err)
	data, err := MarshalPrivateKeyFile(pk)
	require.NoError(t, err)

	parsed, err := ParsePrivateKeyFile(data)
	require.NoError(t, err)
	assert.Equal(t, pk.KeyID, parsed.KeyID)
	assert.Equal(t, pk.Note, parsed.Note)
	assert.Equal(t, pk.Seed, parsed.Seed)
	assert.WithinDuration(t, pk.Created, parsed.Created, time.Second)
}

func TestParsePrivateKeyFileRejectsMismatchedKeyID(t *testing.T) {
	pk, err := GenerateKey("")
	require.NoError(t, err)
	pk.KeyID = "0000000000000000"
	data, err := MarshalPrivateKeyFile(pk)
	require.NoError(t, err)
	_, err = ParsePrivateKeyFile(data)
	assert.Error(t, err)
}

func TestReadPrivateKeyFileRejectsLoosePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check skipped on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.key")
	pk, err := GenerateKey("")
	require.NoError(t, err)
	require.NoError(t, WritePrivateKeyFile(path, pk))

	require.NoError(t, os.Chmod(path, 0o644))
	_, err = ReadPrivateKeyFile(path)
	assert.Error(t, err)

	require.NoError(t, os.Chmod(path, 0o600))
	got, err := ReadPrivateKeyFile(path)
	require.NoError(t, err)
	assert.Equal(t, pk.KeyID, got.KeyID)
}

func TestTrustStoreRoundTrip(t *testing.T) {
	pk, err := GenerateKey("k1")
	require.NoError(t, err)
	pub, err := pk.Public()
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt-pub.json")

	ts := &TrustStore{}
	AddOrReplaceTrustStoreEntry(ts, TrustStoreEntry{
		KeyID:   pk.KeyID,
		Pub:     base64.StdEncoding.EncodeToString(pub),
		Created: time.Now().UTC(),
		Note:    "k1",
	})
	require.NoError(t, SaveTrustStore(path, ts))

	loaded, err := LoadTrustStore(path)
	require.NoError(t, err)
	require.Len(t, loaded.Keys, 1)
	assert.Equal(t, pk.KeyID, loaded.Keys[0].KeyID)

	v, err := NewEd25519Verifier(loaded)
	require.NoError(t, err)
	assert.True(t, v.HasKey(pk.KeyID))

	removed := RemoveTrustStoreEntry(loaded, pk.KeyID)
	assert.True(t, removed)
	assert.Len(t, loaded.Keys, 0)
}

func TestLoadTrustStoreMissingFile(t *testing.T) {
	ts, err := LoadTrustStore(filepath.Join(t.TempDir(), "missing.json"))
	require.NoError(t, err)
	assert.Empty(t, ts.Keys)
}

func TestComputeKeyID(t *testing.T) {
	pk, err := GenerateKey("")
	require.NoError(t, err)
	pub, err := pk.Public()
	require.NoError(t, err)
	assert.Equal(t, KeyIDLen, len(ComputeKeyID(pub)))
	assert.Equal(t, pk.KeyID, ComputeKeyID(pub))
}

func TestSaveTrustStoreLeavesNoTempFiles(t *testing.T) {
	pk, err := GenerateKey("k1")
	require.NoError(t, err)
	pub, err := pk.Public()
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt-pub.json")

	ts := &TrustStore{}
	AddOrReplaceTrustStoreEntry(ts, TrustStoreEntry{
		KeyID:   pk.KeyID,
		Pub:     base64.StdEncoding.EncodeToString(pub),
		Created: time.Now().UTC(),
	})
	require.NoError(t, SaveTrustStore(path, ts))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, ent := range entries {
		assert.NotContains(t, ent.Name(), ".receipt-tmp-", "temp file leaked: %s", ent.Name())
	}

	require.NoError(t, SaveTrustStore(path, ts))
	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "receipt-pub.json", entries[0].Name())
}

func TestWritePrivateKeyFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.key")
	pk, err := GenerateKey("")
	require.NoError(t, err)
	require.NoError(t, WritePrivateKeyFile(path, pk))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "receipt.key", entries[0].Name())
}

// TestParsePrivateKeyFileAcceptsLegacySingleLineBody validates backwards
// compatibility with the pre-PEM on-disk shape that earlier gryph builds
// emitted: header/footer line markers, header keys on individual lines, a
// blank line separator, and the seed as a single base64 line. Existing keys
// on disk written by older builds MUST keep loading.
func TestParsePrivateKeyFileAcceptsLegacySingleLineBody(t *testing.T) {
	pk, err := GenerateKey("legacy-host")
	require.NoError(t, err)

	const (
		header = "-----BEGIN GRYPH-RECEIPT-PRIVATE-KEY-----"
		footer = "-----END GRYPH-RECEIPT-PRIVATE-KEY-----"
	)
	legacy := header + "\n" +
		"keyid: " + pk.KeyID + "\n" +
		"created: " + pk.Created.UTC().Format(time.RFC3339) + "\n" +
		"note: legacy-host\n" +
		"\n" +
		base64.StdEncoding.EncodeToString(pk.Seed) + "\n" +
		footer + "\n"

	parsed, err := ParsePrivateKeyFile([]byte(legacy))
	require.NoError(t, err)
	assert.Equal(t, pk.KeyID, parsed.KeyID)
	assert.Equal(t, "legacy-host", parsed.Note)
	assert.Equal(t, pk.Seed, parsed.Seed)
}

func TestMarshalPrivateKeyFileRejectsNewlineNote(t *testing.T) {
	pk, err := GenerateKey("")
	require.NoError(t, err)
	pk.Note = "with\nnewline"
	_, err = MarshalPrivateKeyFile(pk)
	assert.Error(t, err)
}

// TestReadPrivateKeyFileRejectsSymlink confirms the secure read path refuses
// to follow a symlink whose target may be attacker-controlled. The previous
// implementation stat'd the path and then re-opened it, leaving a window for
// symlink swap; the new path uses O_NOFOLLOW so a symlink at the key path
// fails fast.
func TestReadPrivateKeyFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.key")
	link := filepath.Join(dir, "link.key")
	pk, err := GenerateKey("")
	require.NoError(t, err)
	require.NoError(t, WritePrivateKeyFile(target, pk))

	require.NoError(t, os.Symlink(target, link))
	_, err = ReadPrivateKeyFile(link)
	assert.Error(t, err, "symlinked private key must be rejected by O_NOFOLLOW")
}
