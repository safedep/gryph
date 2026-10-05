package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func machineCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Supervisor.StateDir = filepath.Join(t.TempDir(), "state")
	return cfg
}

func TestEnsureMachineKeys_MakesAndKeeps(t *testing.T) {
	cfg := machineCfg(t)
	first, err := EnsureMachineKeys(cfg)
	require.NoError(t, err)
	assert.True(t, first.Created)
	assert.Len(t, first.KeyID, receipt.KeyIDLen)

	info, err := os.Stat(first.ReceiptKey)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, err = os.Stat(cfg.Supervisor.KeyDir())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(first.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	published, err := receipt.LoadTrustStore(first.PublicKey)
	require.NoError(t, err)
	require.Len(t, published.Keys, 1)
	assert.Equal(t, first.KeyID, published.Keys[0].KeyID)
	_, err = os.Stat(first.ExportKey)
	assert.NoError(t, err)

	second, err := EnsureMachineKeys(cfg)
	require.NoError(t, err)
	assert.False(t, second.Created)
	assert.Equal(t, first.KeyID, second.KeyID, "an existing key stays")
}

func TestRotateMachineKey_AndTrust(t *testing.T) {
	cfg := machineCfg(t)
	first, err := EnsureMachineKeys(cfg)
	require.NoError(t, err)
	store := filepath.Join(t.TempDir(), "managed", "keys", "receipt-pub.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(store), 0o755))

	keyID, changed, err := TrustMachineKey(cfg, store)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, first.KeyID, keyID)
	_, changed, err = TrustMachineKey(cfg, store)
	require.NoError(t, err)
	assert.False(t, changed, "a second run changes nothing")

	rotated, err := RotateMachineKey(cfg, "test rotation")
	require.NoError(t, err)
	assert.NotEqual(t, first.KeyID, rotated.KeyID)
	published, err := receipt.LoadTrustStore(rotated.PublicKey)
	require.NoError(t, err)
	require.Len(t, published.Keys, 2, "the old public half stays published")
	assert.Equal(t, first.KeyID, published.Keys[0].KeyID)
	assert.Equal(t, rotated.KeyID, published.Keys[1].KeyID)
	assert.Equal(t, "test rotation", published.Keys[1].Note)

	kept, err := EnsureMachineKeys(cfg)
	require.NoError(t, err)
	assert.False(t, kept.Created)
	assert.Equal(t, rotated.KeyID, kept.KeyID, "the current key is the last published one")
	require.NoError(t, os.Remove(rotated.PublicKey))
	_, err = EnsureMachineKeys(cfg)
	assert.Error(t, err, "a private key with no published half is an error, not a read of the key")
	require.NoError(t, publishPublicKey(rotated.PublicKey, &receipt.PrivateKeyFile{KeyID: rotated.KeyID, Seed: mustSeed(t, cfg)}))

	keyID, changed, err = TrustMachineKey(cfg, store)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, rotated.KeyID, keyID)
	ts, err := receipt.LoadTrustStore(store)
	require.NoError(t, err)
	require.Len(t, ts.Keys, 2, "the old key stays, so its receipts verify")

	_, _, err = TrustMachineKey(cfg, "")
	assert.Error(t, err)
}

func TestPartitionConfig_UsesTheMachineKey(t *testing.T) {
	cfg := machineCfg(t)
	cfg.Policy.Receipts.SignMode = config.SignModeAuto
	pcfg := PartitionConfig(cfg)
	assert.Equal(t, cfg.Supervisor.ReceiptKeyPath(), pcfg.Policy.Receipts.KeyPath)
	assert.Equal(t, config.SignModeAlways, pcfg.Policy.Receipts.SignMode)
	assert.Equal(t, receipt.KeyScopeSupervisor, pcfg.Policy.Receipts.KeyScope)
	assert.Equal(t, cfg.Supervisor.ExportKeyPath(), pcfg.ExportKeyFile())
	assert.Empty(t, cfg.Policy.Receipts.KeyPath, "the input is not changed")

	cfg.Policy.Receipts.KeyPath = "/managed/other.key"
	assert.Equal(t, "/managed/other.key", PartitionConfig(cfg).Policy.Receipts.KeyPath, "the managed file names the key")

	_, err := EnsureMachineKeys(cfg)
	require.NoError(t, err)
	cfg.Policy.Receipts.KeyPath = ""
	signer, err := LoadReceiptSignerFromConfig(PartitionConfig(cfg), nil)
	require.NoError(t, err)
	assert.Equal(t, receipt.KeyScopeSupervisor, receipt.ScopeOf(signer))
}

func mustSeed(t *testing.T, cfg *config.Config) []byte {
	t.Helper()
	pk, err := receipt.ReadPrivateKeyFile(cfg.Supervisor.ReceiptKeyPath())
	require.NoError(t, err)
	return pk.Seed
}
