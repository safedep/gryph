package engine

import (
	"fmt"
	"os"
	"sync"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
)

// LoadReceiptVerifierFromConfig loads the configured trust store and the
// managed one. Returns an empty verifier when both are missing.
func LoadReceiptVerifierFromConfig(cfg *config.Config, paths *config.Paths) (*receipt.Ed25519Verifier, error) {
	ts, err := receipt.LoadTrustStores(cfg.ReceiptTrustStorePaths(paths)...)
	if err != nil {
		return nil, fmt.Errorf("load trust store: %w", err)
	}
	return receipt.NewEd25519Verifier(ts)
}

// signerAutoMissingKeyOnce guards the one-time log line emitted when
// sign_mode=auto is selected but no key file is present on disk.
var signerAutoMissingKeyOnce sync.Once

// LoadReceiptSignerFromConfig loads the configured signing key into an
// Ed25519Signer. The behavior depends on policy.receipts.sign_mode:
//   - never: return (nil, nil) unconditionally
//   - always: load the key. Hard-fail when the key is missing
//   - auto (default): load the key if present, otherwise log once and
//     return (nil, nil) so the mediator writes unsigned receipts.
func LoadReceiptSignerFromConfig(cfg *config.Config, paths *config.Paths) (*receipt.Ed25519Signer, error) {
	if cfg == nil {
		return nil, nil
	}
	mode := cfg.Policy.Receipts.EffectiveSignMode()
	if mode == config.SignModeNever {
		return nil, nil
	}
	keyPath := cfg.ResolveReceiptKeyPath(paths)
	if mode == config.SignModeAuto {
		if _, err := os.Stat(keyPath); err != nil {
			if os.IsNotExist(err) {
				signerAutoMissingKeyOnce.Do(func() {
					log.Warnf("config: policy.receipts.sign_mode=auto but no key at %v; receipts will be unsigned", keyPath)
				})
				return nil, nil
			}
			return nil, fmt.Errorf("stat private key %s: %w", keyPath, err)
		}
	}
	pkFile, err := receipt.ReadPrivateKeyFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", keyPath, err)
	}
	priv, err := pkFile.PrivateKey()
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	signer, err := receipt.NewEd25519Signer(priv)
	if err != nil {
		return nil, err
	}
	if scope := cfg.Policy.Receipts.KeyScope; scope != "" {
		signer = signer.WithScope(scope)
	}
	return signer, nil
}
