package engine

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
)

// MachineKeys is the state of the keys of the decision service after
// EnsureMachineKeys or RotateMachineKey.
type MachineKeys struct {
	// ReceiptKey is the private signing key, and KeyID the id of its
	// public half.
	ReceiptKey string
	KeyID      string
	// PublicKey is the file that carries the public halves of the keys
	// the service has had, the current one last, readable by every
	// account.
	PublicKey string
	ExportKey string
	// Created is true when this call made the receipt key.
	Created bool
}

// EnsureMachineKeys makes the machine keys when they are missing: the
// receipt signing key, its public half next to it, and the export key,
// all under the key directory of the state directory, readable by the
// owner alone except the public half. The decision service runs it at
// start, as the service account, and the managed install runs it as root
// before it hands the files to the service account. A key that exists
// stays, and is not read: root takes the key id from the public half, so
// a key the service account owns stays closed to everyone else.
func EnsureMachineKeys(cfg *config.Config) (*MachineKeys, error) {
	sup := cfg.Supervisor
	keys := &MachineKeys{ReceiptKey: sup.ReceiptKeyPath(), PublicKey: sup.PublicKeyPath(), ExportKey: sup.ExportKeyPath()}
	if err := os.MkdirAll(sup.KeyDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create the key directory: %w", err)
	}
	info, err := os.Lstat(keys.ReceiptKey)
	switch {
	case errors.Is(err, os.ErrNotExist):
		pk, err := writeNewReceiptKey(keys.ReceiptKey, "machine key")
		if err != nil {
			return nil, err
		}
		keys.Created = true
		keys.KeyID = pk.KeyID
		if err := publishPublicKey(keys.PublicKey, pk); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", keys.ReceiptKey)
	default:
		keys.KeyID, err = currentPublishedKey(keys.PublicKey)
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Lstat(keys.ExportKey); errors.Is(err, os.ErrNotExist) {
		if _, err := config.LoadOrCreateExportKey(keys.ExportKey); err != nil {
			return nil, fmt.Errorf("export key: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	return keys, nil
}

// RotateMachineKey replaces the receipt signing key with a new one and
// adds its public half to the published ones. The old key signs nothing
// after the service reloads. Its public half stays published and in the
// trust store, so the receipts it signed still verify.
func RotateMachineKey(cfg *config.Config, note string) (*MachineKeys, error) {
	sup := cfg.Supervisor
	keys := &MachineKeys{ReceiptKey: sup.ReceiptKeyPath(), PublicKey: sup.PublicKeyPath(), ExportKey: sup.ExportKeyPath(), Created: true}
	if err := os.MkdirAll(sup.KeyDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create the key directory: %w", err)
	}
	pk, err := writeNewReceiptKey(keys.ReceiptKey, note)
	if err != nil {
		return nil, err
	}
	keys.KeyID = pk.KeyID
	if err := publishPublicKey(keys.PublicKey, pk); err != nil {
		return nil, err
	}
	return keys, nil
}

// currentPublishedKey returns the id of the last published key: the one
// the private key file holds.
func currentPublishedKey(path string) (string, error) {
	published, err := receipt.LoadTrustStore(path)
	if err != nil {
		return "", err
	}
	if len(published.Keys) == 0 {
		return "", fmt.Errorf("%s holds no key next to the private key. Run `gryph supervisor keys rotate`", path)
	}
	return published.Keys[len(published.Keys)-1].KeyID, nil
}

func writeNewReceiptKey(path, note string) (*receipt.PrivateKeyFile, error) {
	pk, err := receipt.GenerateKey(note)
	if err != nil {
		return nil, err
	}
	if err := receipt.WritePrivateKeyFile(path, pk); err != nil {
		return nil, err
	}
	return pk, nil
}

// publishPublicKey adds the trust store entry of pk to the published keys
// at path, last, with mode 0644 on the file.
func publishPublicKey(path string, pk *receipt.PrivateKeyFile) error {
	pub, err := pk.Public()
	if err != nil {
		return err
	}
	ts, err := receipt.LoadTrustStore(path)
	if err != nil {
		return err
	}
	receipt.AddOrReplaceTrustStoreEntry(ts, receipt.TrustStoreEntry{
		KeyID:   pk.KeyID,
		Pub:     base64.StdEncoding.EncodeToString(pub),
		Created: pk.Created,
		Note:    pk.Note,
	})
	return receipt.SaveTrustStore(path, ts)
}

// TrustMachineKey puts the public half of the machine key into the trust
// store at store, next to the keys already there. Root runs it on the
// managed store: the store is the system's, and a verifier of any account
// trusts what it holds. It reads the public half only, never the private
// key, so it works when the service account owns the key. It reports
// whether the store changed.
func TrustMachineKey(cfg *config.Config, store string) (keyID string, changed bool, err error) {
	if store == "" {
		return "", false, errors.New("this platform has no managed trust store")
	}
	published, err := receipt.LoadTrustStore(cfg.Supervisor.PublicKeyPath())
	if err != nil {
		return "", false, err
	}
	if len(published.Keys) == 0 {
		return "", false, fmt.Errorf("%s holds no key", cfg.Supervisor.PublicKeyPath())
	}
	managed, err := receipt.LoadTrustStore(store)
	if err != nil {
		return "", false, err
	}
	for _, entry := range published.Keys {
		if _, err := receipt.ValidateTrustEntry(entry); err != nil {
			return "", false, fmt.Errorf("published key %s: %w", entry.KeyID, err)
		}
		receipt.AddOrReplaceTrustStoreEntry(managed, entry)
		keyID = entry.KeyID
	}
	data, err := receipt.MarshalTrustStore(managed)
	if err != nil {
		return "", false, err
	}
	changed, err = config.WriteManagedFile(store, data)
	return keyID, changed, err
}
