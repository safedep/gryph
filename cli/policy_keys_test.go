package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestKey(t *testing.T, path string) {
	t.Helper()
	pk, err := receipt.GenerateKey("test")
	require.NoError(t, err)
	require.NoError(t, receipt.WritePrivateKeyFile(path, pk))
}

func newSignerConfigWithMode(mode, keyPath string) (*config.Config, *config.Paths) {
	cfg := config.Default()
	cfg.Policy.Receipts.SignMode = mode
	cfg.Policy.Receipts.KeyPath = keyPath
	cfg.Policy.Receipts.Sign = false
	return cfg, &config.Paths{}
}

func TestLoadReceiptSigner_NeverReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "missing.key")
	cfg, paths := newSignerConfigWithMode(config.SignModeNever, keyPath)

	signer, err := loadReceiptSignerFromConfig(cfg, paths)
	require.NoError(t, err)
	assert.Nil(t, signer)
}

func TestLoadReceiptSigner_AutoMissingKeyReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "missing.key")
	cfg, paths := newSignerConfigWithMode(config.SignModeAuto, keyPath)

	signer, err := loadReceiptSignerFromConfig(cfg, paths)
	require.NoError(t, err)
	assert.Nil(t, signer, "auto with no key must return a nil signer")
}

func TestLoadReceiptSigner_AutoWithKeyLoadsSigner(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "receipt.key")
	writeTestKey(t, keyPath)
	cfg, paths := newSignerConfigWithMode(config.SignModeAuto, keyPath)

	signer, err := loadReceiptSignerFromConfig(cfg, paths)
	require.NoError(t, err)
	require.NotNil(t, signer)
	assert.NotEmpty(t, signer.KeyID())
}

func TestLoadReceiptSigner_AlwaysMissingKeyFails(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "missing.key")
	cfg, paths := newSignerConfigWithMode(config.SignModeAlways, keyPath)

	signer, err := loadReceiptSignerFromConfig(cfg, paths)
	assert.Error(t, err, "always must hard-fail when the key is absent")
	assert.Nil(t, signer)
}

func TestLoadReceiptSigner_AlwaysWithKeyLoadsSigner(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "receipt.key")
	writeTestKey(t, keyPath)
	cfg, paths := newSignerConfigWithMode(config.SignModeAlways, keyPath)

	signer, err := loadReceiptSignerFromConfig(cfg, paths)
	require.NoError(t, err)
	require.NotNil(t, signer)
}

func TestLoadReceiptSigner_LegacySignBoolAliasesAlways(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "receipt.key")
	writeTestKey(t, keyPath)

	configFile := filepath.Join(tmp, "config.yaml")
	yaml := fmt.Sprintf(`
policy:
  enabled: true
  receipts:
    sign: true
    key_path: %q
`, keyPath)
	require.NoError(t, os.WriteFile(configFile, []byte(yaml), 0o600))

	cfg, err := config.Load(configFile)
	require.NoError(t, err)
	require.Equal(t, config.SignModeAlways, cfg.Policy.Receipts.SignMode,
		"Load must normalize legacy sign:true to sign_mode=always")
	require.Equal(t, config.SignModeAlways, cfg.Policy.Receipts.EffectiveSignMode())

	signer, err := loadReceiptSignerFromConfig(cfg, &config.Paths{})
	require.NoError(t, err)
	require.NotNil(t, signer)
}
