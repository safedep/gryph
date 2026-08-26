package receipt

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// KeyIDLen is the number of hex characters in a key ID (8 bytes of SHA-256
	// of the public key, hex-encoded -> 16 hex chars).
	KeyIDLen = 16

	privateKeyPEMType = "GRYPH-RECEIPT-PRIVATE-KEY"
)

// Signer produces an Ed25519 signature over a receipt hash and returns the
// signature alongside the keyID that identifies the signing public key.
type Signer interface {
	Sign(hash []byte) (signature []byte, keyID string, err error)
}

// Verifier checks a receipt signature against a trusted public key identified
// by keyID. HasKey reports whether the verifier knows the keyID.
type Verifier interface {
	Verify(hash, signature []byte, keyID string) error
	HasKey(keyID string) bool
}

// Ed25519Signer signs receipt hashes with an in-memory Ed25519 private key.
type Ed25519Signer struct {
	priv  ed25519.PrivateKey
	keyID string
}

// Ed25519Verifier verifies receipt signatures against a keyID-indexed pubkey
// map. The map is populated from the trust store loaded by LoadTrustStore.
type Ed25519Verifier struct {
	keys map[string]ed25519.PublicKey
}

// PrivateKeyFile is the parsed representation of the on-disk private key file.
type PrivateKeyFile struct {
	KeyID   string
	Created time.Time
	Note    string
	Seed    []byte
}

// TrustStoreEntry is one trusted public key in the trust store.
type TrustStoreEntry struct {
	KeyID   string    `json:"key_id"`
	Pub     string    `json:"pub"`
	Created time.Time `json:"created"`
	Note    string    `json:"note,omitempty"`
}

// TrustStore is the on-disk JSON shape of the trust store: an ordered list of
// trusted public keys. The verifier indexes by KeyID for O(1) lookup.
type TrustStore struct {
	Keys []TrustStoreEntry `json:"keys"`
}

// ComputeKeyID returns the lowercase hex of sha256(pub)[0:8], the canonical
// 16-character key ID used in the receipt and the trust store.
func ComputeKeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// NewEd25519Signer returns a signer that uses priv. The keyID is derived from
// the embedded public key.
func NewEd25519Signer(priv ed25519.PrivateKey) (*Ed25519Signer, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("receipt: invalid ed25519 private key length: got %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("receipt: failed to extract public key from private key")
	}
	return &Ed25519Signer{priv: priv, keyID: ComputeKeyID(pub)}, nil
}

// Sign implements Signer.
func (s *Ed25519Signer) Sign(hash []byte) ([]byte, string, error) {
	if s == nil || len(s.priv) == 0 {
		return nil, "", errors.New("receipt: signer is not initialized")
	}
	if len(hash) == 0 {
		return nil, "", errors.New("receipt: cannot sign empty hash")
	}
	sig := ed25519.Sign(s.priv, hash)
	return sig, s.keyID, nil
}

// KeyID returns the keyID this signer signs under.
func (s *Ed25519Signer) KeyID() string {
	if s == nil {
		return ""
	}
	return s.keyID
}

// ValidateTrustEntry checks that a trust store entry has a syntactically valid
// key_id, a base64-decodable pub of the right length, and that the key_id
// matches sha256(pub)[:8]. Returns the decoded pub on success.
func ValidateTrustEntry(e TrustStoreEntry) (ed25519.PublicKey, error) {
	if len(e.KeyID) != KeyIDLen {
		return nil, fmt.Errorf("invalid key_id length: got %d, want %d", len(e.KeyID), KeyIDLen)
	}
	pub, err := base64.StdEncoding.DecodeString(e.Pub)
	if err != nil {
		return nil, fmt.Errorf("decode pub: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid pub length: got %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if ComputeKeyID(pub) != strings.ToLower(e.KeyID) {
		return nil, errors.New("key_id does not match pub")
	}
	return pub, nil
}

// NewEd25519Verifier returns a verifier backed by the supplied trust store.
func NewEd25519Verifier(store *TrustStore) (*Ed25519Verifier, error) {
	keys := map[string]ed25519.PublicKey{}
	if store != nil {
		for i, e := range store.Keys {
			pub, err := ValidateTrustEntry(e)
			if err != nil {
				return nil, fmt.Errorf("receipt: trust store entry %d: %w", i, err)
			}
			keys[strings.ToLower(e.KeyID)] = pub
		}
	}
	return &Ed25519Verifier{keys: keys}, nil
}

// HasKey implements Verifier.
func (v *Ed25519Verifier) HasKey(keyID string) bool {
	if v == nil || v.keys == nil {
		return false
	}
	_, ok := v.keys[strings.ToLower(keyID)]
	return ok
}

// Verify implements Verifier.
func (v *Ed25519Verifier) Verify(hash, signature []byte, keyID string) error {
	if v == nil || v.keys == nil {
		return errors.New("receipt: verifier is not initialized")
	}
	pub, ok := v.keys[strings.ToLower(keyID)]
	if !ok {
		return fmt.Errorf("receipt: unknown key_id %q", keyID)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("receipt: invalid signature length: got %d, want %d", len(signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(pub, hash, signature) {
		return errors.New("receipt: signature verification failed")
	}
	return nil
}

// GenerateKey produces a fresh Ed25519 keypair and returns the PrivateKeyFile
// representation (seed + derived keyID + creation timestamp). The matching
// public key is recoverable from the seed via ed25519.NewKeyFromSeed.
func GenerateKey(note string) (*PrivateKeyFile, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("receipt: generate ed25519 key: %w", err)
	}
	seed := priv.Seed()
	return &PrivateKeyFile{
		KeyID:   ComputeKeyID(pub),
		Created: time.Now().UTC(),
		Note:    note,
		Seed:    seed,
	}, nil
}

// Public returns the ed25519 public key derived from the file's seed.
func (p *PrivateKeyFile) Public() (ed25519.PublicKey, error) {
	priv, err := p.PrivateKey()
	if err != nil {
		return nil, err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("receipt: failed to derive public key")
	}
	return pub, nil
}

// PrivateKey returns the ed25519.PrivateKey reconstructed from the seed.
func (p *PrivateKeyFile) PrivateKey() (ed25519.PrivateKey, error) {
	if p == nil || len(p.Seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("receipt: invalid seed length: got %d, want %d", len(p.Seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(p.Seed), nil
}

// MarshalPrivateKeyFile serializes the private key file to a standard PEM
// block (RFC 1421-style) carrying the seed bytes with keyid/created/note in
// the PEM headers.
func MarshalPrivateKeyFile(p *PrivateKeyFile) ([]byte, error) {
	if p == nil {
		return nil, errors.New("receipt: nil private key file")
	}
	if len(p.Seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("receipt: invalid seed length: got %d, want %d", len(p.Seed), ed25519.SeedSize)
	}
	if p.KeyID == "" {
		pub := ed25519.NewKeyFromSeed(p.Seed).Public().(ed25519.PublicKey)
		p.KeyID = ComputeKeyID(pub)
	}
	created := p.Created
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if strings.ContainsAny(p.Note, "\r\n") {
		return nil, errors.New("receipt: note must not contain newlines")
	}
	headers := map[string]string{
		"keyid":   p.KeyID,
		"created": created.UTC().Format(time.RFC3339),
	}
	if p.Note != "" {
		headers["note"] = p.Note
	}
	block := &pem.Block{
		Type:    privateKeyPEMType,
		Headers: headers,
		Bytes:   p.Seed,
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, block); err != nil {
		return nil, fmt.Errorf("receipt: encode pem: %w", err)
	}
	return buf.Bytes(), nil
}

// ParsePrivateKeyFile parses the PEM private key file format. It also accepts
// the legacy single-line-base64 body shape that pre-PEM gryph builds emitted,
// so existing on-disk keys continue to load.
func ParsePrivateKeyFile(data []byte) (*PrivateKeyFile, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return parseLegacyPrivateKeyFile(data)
	}
	if block.Type != privateKeyPEMType {
		return nil, fmt.Errorf("receipt: unexpected PEM type %q", block.Type)
	}
	if len(block.Bytes) == 0 {
		return nil, errors.New("receipt: empty key body")
	}
	if len(block.Bytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("receipt: invalid seed length: got %d, want %d", len(block.Bytes), ed25519.SeedSize)
	}
	p := &PrivateKeyFile{
		KeyID: strings.ToLower(block.Headers["keyid"]),
		Note:  block.Headers["note"],
		Seed:  block.Bytes,
	}
	if v := block.Headers["created"]; v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("receipt: parse created: %w", err)
		}
		p.Created = t
	}
	pub := ed25519.NewKeyFromSeed(block.Bytes).Public().(ed25519.PublicKey)
	derived := ComputeKeyID(pub)
	if p.KeyID == "" {
		p.KeyID = derived
	} else if p.KeyID != derived {
		return nil, fmt.Errorf("receipt: keyid header %q does not match derived %q", p.KeyID, derived)
	}
	return p, nil
}

// parseLegacyPrivateKeyFile parses the pre-PEM single-line-base64 body form
// that earlier builds wrote. Kept so existing on-disk keys continue to load.
func parseLegacyPrivateKeyFile(data []byte) (*PrivateKeyFile, error) {
	const (
		legacyHeader = "-----BEGIN " + privateKeyPEMType + "-----"
		legacyFooter = "-----END " + privateKeyPEMType + "-----"
	)
	lines := strings.Split(string(data), "\n")
	var (
		inBlock    bool
		sawHeader  bool
		sawFooter  bool
		headerMap  = map[string]string{}
		bodyB64    strings.Builder
		readingHdr = true
	)
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == legacyHeader:
			if sawFooter {
				return nil, errors.New("receipt: header after footer")
			}
			inBlock = true
			sawHeader = true
		case trimmed == legacyFooter:
			if !sawHeader {
				return nil, errors.New("receipt: footer before header")
			}
			sawFooter = true
		case inBlock && !sawFooter:
			if trimmed == "" {
				readingHdr = false
				continue
			}
			if readingHdr && strings.Contains(line, ":") {
				parts := strings.SplitN(line, ":", 2)
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				if k != "" {
					headerMap[strings.ToLower(k)] = v
				}
				continue
			}
			readingHdr = false
			bodyB64.WriteString(trimmed)
		}
	}
	if !sawFooter {
		return nil, errors.New("receipt: missing end-of-key marker")
	}
	if bodyB64.Len() == 0 {
		return nil, errors.New("receipt: empty key body")
	}
	seed, err := base64.StdEncoding.DecodeString(bodyB64.String())
	if err != nil {
		return nil, fmt.Errorf("receipt: decode seed: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("receipt: invalid seed length: got %d, want %d", len(seed), ed25519.SeedSize)
	}
	p := &PrivateKeyFile{
		KeyID: strings.ToLower(headerMap["keyid"]),
		Note:  headerMap["note"],
		Seed:  seed,
	}
	if v := headerMap["created"]; v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("receipt: parse created: %w", err)
		}
		p.Created = t
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	derived := ComputeKeyID(pub)
	if p.KeyID == "" {
		p.KeyID = derived
	} else if p.KeyID != derived {
		return nil, fmt.Errorf("receipt: keyid header %q does not match derived %q", p.KeyID, derived)
	}
	return p, nil
}

// ReadPrivateKeyFile loads and parses the private key file at path. On Unix
// platforms it opens the file with O_NOFOLLOW and refuses to load when the
// file mode is broader than 0600 or the owner is not the current user. The
// open-then-stat-then-read sequence operates on a single file descriptor to
// avoid a symlink-swap TOCTOU between perm check and read.
func ReadPrivateKeyFile(path string) (*PrivateKeyFile, error) {
	data, err := readPrivateKeyFileSecure(path)
	if err != nil {
		return nil, err
	}
	return ParsePrivateKeyFile(data)
}

// WritePrivateKeyFile serializes p and writes it to path with 0600 mode. The
// parent directory is created with 0700 if missing.
func WritePrivateKeyFile(path string, p *PrivateKeyFile) error {
	data, err := MarshalPrivateKeyFile(p)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("receipt: create key dir: %w", err)
		}
	}
	if err := writeAtomicFile(path, data, 0o600); err != nil {
		return fmt.Errorf("receipt: write private key: %w", err)
	}
	return nil
}

// writeAtomicFile writes data to path via a same-directory temp file, fsyncs
// it, and renames into place. A crash mid-write leaves the original file
// untouched. On any failure the temp file is removed so partial state does
// not leak into the target directory.
func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".receipt-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// LoadTrustStore reads a trust store JSON file. Returns an empty TrustStore
// when the file does not exist so callers can create it lazily.
func LoadTrustStore(path string) (*TrustStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TrustStore{}, nil
		}
		return nil, fmt.Errorf("receipt: read trust store: %w", err)
	}
	return ParseTrustStore(data)
}

// ParseTrustStore parses the trust store JSON byte payload.
func ParseTrustStore(data []byte) (*TrustStore, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &TrustStore{}, nil
	}
	var ts TrustStore
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("receipt: parse trust store: %w", err)
	}
	for i := range ts.Keys {
		ts.Keys[i].KeyID = strings.ToLower(ts.Keys[i].KeyID)
	}
	return &ts, nil
}

// SaveTrustStore serializes ts to JSON and writes it to path with 0644
// permissions. The parent directory is created with 0700 if missing.
func SaveTrustStore(path string, ts *TrustStore) error {
	if ts == nil {
		ts = &TrustStore{}
	}
	for i := range ts.Keys {
		ts.Keys[i].KeyID = strings.ToLower(ts.Keys[i].KeyID)
	}
	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return fmt.Errorf("receipt: marshal trust store: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("receipt: create trust store dir: %w", err)
		}
	}
	if err := writeAtomicFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("receipt: write trust store: %w", err)
	}
	return nil
}

// AddOrReplaceTrustStoreEntry inserts entry into ts, replacing any existing
// entry with the same key_id.
func AddOrReplaceTrustStoreEntry(ts *TrustStore, entry TrustStoreEntry) {
	entry.KeyID = strings.ToLower(entry.KeyID)
	for i := range ts.Keys {
		if strings.ToLower(ts.Keys[i].KeyID) == entry.KeyID {
			ts.Keys[i] = entry
			return
		}
	}
	ts.Keys = append(ts.Keys, entry)
}

// RemoveTrustStoreEntry deletes the entry with key_id == keyID from ts.
// Returns true if a row was removed.
func RemoveTrustStoreEntry(ts *TrustStore, keyID string) bool {
	keyID = strings.ToLower(keyID)
	for i := range ts.Keys {
		if strings.ToLower(ts.Keys[i].KeyID) == keyID {
			ts.Keys = append(ts.Keys[:i], ts.Keys[i+1:]...)
			return true
		}
	}
	return false
}
