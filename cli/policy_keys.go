package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func newPolicyKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage Ed25519 keys used to sign AARM receipts",
		Long: "Generate, list, trust, and revoke the per-host signing keys " +
			"that produce receipt signatures. The private key lives at " +
			"~/.config/gryph/keys/receipt.key (mode 0600). Public keys " +
			"the verifier trusts live in ~/.config/gryph/keys/receipt-pub.json. " +
			"Signing defaults to sign_mode: auto: receipts are signed when a key " +
			"is present and unsigned when not. Set sign_mode: always to require a key.",
	}
	cmd.AddCommand(
		newPolicyKeysGenerateCmd(),
		newPolicyKeysListCmd(),
		newPolicyKeysTrustCmd(),
		newPolicyKeysRevokeCmd(),
	)
	return cmd
}

func newPolicyKeysGenerateCmd() *cobra.Command {
	var (
		note  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new Ed25519 signing keypair",
		Long: "Generates a fresh keypair. The private key is written to the " +
			"configured key_path (or the platform default). The public key is " +
			"appended to the trust store so the verifier accepts signatures " +
			"from it. Refuses to overwrite an existing private key unless " +
			"--force is passed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			keyPath := app.Config.ResolveReceiptKeyPath(app.Paths)
			trustPath := app.Config.ResolveReceiptTrustStorePath(app.Paths)

			rotated := false
			if _, statErr := os.Stat(keyPath); statErr == nil {
				if !force {
					return ErrConfig("refusing to overwrite existing key (use --force to replace)", fmt.Errorf("%s exists", keyPath))
				}
				rotated = true
			}

			pkFile, err := receipt.GenerateKey(note)
			if err != nil {
				return ErrConfig("generate key", err)
			}
			if err := receipt.WritePrivateKeyFile(keyPath, pkFile); err != nil {
				return ErrConfig("write private key", err)
			}

			pub, err := pkFile.Public()
			if err != nil {
				return ErrConfig("derive public key", err)
			}
			ts, err := receipt.LoadTrustStore(trustPath)
			if err != nil {
				return ErrConfig("load trust store", err)
			}
			receipt.AddOrReplaceTrustStoreEntry(ts, receipt.TrustStoreEntry{
				KeyID:   pkFile.KeyID,
				Pub:     base64.StdEncoding.EncodeToString(pub),
				Created: pkFile.Created,
				Note:    note,
			})
			if err := receipt.SaveTrustStore(trustPath, ts); err != nil {
				return ErrConfig("write trust store", err)
			}

			if rotated {
				if err := app.InitStore(cmd.Context()); err == nil {
					defer func() {
						if cerr := app.Close(); cerr != nil {
							log.Errorf("failed to close app: %v", cerr)
						}
					}()
					details := map[string]interface{}{
						"key_id":      pkFile.KeyID,
						"key_path":    keyPath,
						"trust_store": trustPath,
					}
					if note != "" {
						details["note"] = note
					}
					if err := logSelfAudit(cmd.Context(), app.Store, SelfAuditActionReceiptKeyRotated, "",
						details, SelfAuditResultSuccess, ""); err != nil {
						log.Errorf("failed to record receipt_key_rotated audit: %v", err)
					}
				}
			}

			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s Generated key %s -> %s\n", c.StatusOK(), c.Cyan(pkFile.KeyID), c.Path(keyPath))
			if app.Config.Policy.Receipts.EffectiveSignMode() == config.SignModeNever {
				_, _ = fmt.Fprintln(out, c.Dim("Set policy.receipts.sign_mode: auto (default) or always to start signing receipts."))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "optional note stored alongside the key")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing key (records a receipt_key_rotated audit)")
	return cmd
}

func newPolicyKeysListCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trusted public keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			trustPath := app.Config.ResolveReceiptTrustStorePath(app.Paths)
			ts, err := receipt.LoadTrustStore(trustPath)
			if err != nil {
				return ErrConfig("load trust store", err)
			}

			out := cmd.OutOrStdout()
			if format == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(ts)
			}
			renderKeysTable(out, policyColorizer(app), ts, trustPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	return cmd
}

func newPolicyKeysTrustCmd() *cobra.Command {
	var pubPath string
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Add an external public key to the trust store",
		Long: "Reads a JSON document with key_id, pub (base64), created, and " +
			"optional note from --pub and appends it to the trust store.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pubPath == "" {
				return ErrConfig("--pub is required", fmt.Errorf("missing public key file"))
			}
			app, err := loadApp()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(pubPath)
			if err != nil {
				return ErrConfig("read public key", err)
			}
			var entry receipt.TrustStoreEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return ErrConfig("parse public key", err)
			}
			if entry.KeyID == "" || entry.Pub == "" {
				return ErrConfig("public key file is incomplete", fmt.Errorf("key_id and pub are required"))
			}
			if _, verr := receipt.ValidateTrustEntry(entry); verr != nil {
				return ErrConfig("invalid public key entry", verr)
			}
			if entry.Created.IsZero() {
				entry.Created = time.Now().UTC()
			}
			trustPath := app.Config.ResolveReceiptTrustStorePath(app.Paths)
			ts, err := receipt.LoadTrustStore(trustPath)
			if err != nil {
				return ErrConfig("load trust store", err)
			}
			receipt.AddOrReplaceTrustStoreEntry(ts, entry)
			if err := receipt.SaveTrustStore(trustPath, ts); err != nil {
				return ErrConfig("write trust store", err)
			}
			c := policyColorizer(app)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Trusted key %s\n", c.StatusOK(), c.Cyan(strings.ToLower(entry.KeyID)))
			return nil
		},
	}
	cmd.Flags().StringVar(&pubPath, "pub", "", "path to a JSON file with {key_id, pub, created, note}")
	return cmd
}

func newPolicyKeysRevokeCmd() *cobra.Command {
	var keyID string
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Remove a public key from the trust store",
		Long: "Removes the key with the given --key-id from the trust store. " +
			"Does not delete the private key file; the verifier just stops " +
			"accepting signatures from this key.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keyID == "" {
				return ErrConfig("--key-id is required", fmt.Errorf("missing key id"))
			}
			app, err := loadApp()
			if err != nil {
				return err
			}
			trustPath := app.Config.ResolveReceiptTrustStorePath(app.Paths)
			ts, err := receipt.LoadTrustStore(trustPath)
			if err != nil {
				return ErrConfig("load trust store", err)
			}
			removed := receipt.RemoveTrustStoreEntry(ts, keyID)
			if !removed {
				return ErrConfig("key not found in trust store", fmt.Errorf("key_id=%s", keyID))
			}
			if err := receipt.SaveTrustStore(trustPath, ts); err != nil {
				return ErrConfig("write trust store", err)
			}
			c := policyColorizer(app)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Revoked key %s\n", c.StatusOK(), c.Cyan(strings.ToLower(keyID)))
			return nil
		},
	}
	cmd.Flags().StringVar(&keyID, "key-id", "", "key ID to remove from the trust store")
	return cmd
}

func renderKeysTable(w io.Writer, c *tui.Colorizer, ts *receipt.TrustStore, path string) {
	if ts == nil || len(ts.Keys) == 0 {
		_, _ = fmt.Fprintf(w, "%s (%s)\n", c.Dim("no trusted keys"), c.Path(path))
		return
	}
	_, _ = fmt.Fprintf(w, "%-16s  %-20s  %s\n", c.Dim("key_id"), c.Dim("created"), c.Dim("note"))
	for _, k := range ts.Keys {
		created := ""
		if !k.Created.IsZero() {
			created = k.Created.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%-16s  %-20s  %s\n", k.KeyID, created, tui.TruncateString(k.Note, 40))
	}
}

// loadReceiptVerifierFromConfig loads the trust store for the configured
// path. Returns an empty verifier when the trust store is missing.
func loadReceiptVerifierFromConfig(cfg *config.Config, paths *config.Paths) (*receipt.Ed25519Verifier, error) {
	trustPath := cfg.ResolveReceiptTrustStorePath(paths)
	ts, err := receipt.LoadTrustStore(trustPath)
	if err != nil {
		return nil, fmt.Errorf("load trust store %s: %w", trustPath, err)
	}
	return receipt.NewEd25519Verifier(ts)
}

// signerAutoMissingKeyOnce guards the one-time log line emitted when
// sign_mode=auto is selected but no key file is present on disk.
var signerAutoMissingKeyOnce sync.Once

// loadReceiptSignerFromConfig loads the configured signing key into an
// Ed25519Signer. The behavior depends on policy.receipts.sign_mode:
//   - never: return (nil, nil) unconditionally
//   - always: load the key. Hard-fail when the key is missing
//   - auto (default): load the key if present, otherwise log once and
//     return (nil, nil) so the mediator writes unsigned receipts.
func loadReceiptSignerFromConfig(cfg *config.Config, paths *config.Paths) (*receipt.Ed25519Signer, error) {
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
	return receipt.NewEd25519Signer(priv)
}
