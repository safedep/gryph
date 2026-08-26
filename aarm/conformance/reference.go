package conformance

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/classify"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/injectscore"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/require"
)

// referenceConfig collects the optional dependencies tunable via Option.
type referenceConfig struct {
	policyPath        string
	policyBody        []byte
	humanPrincipal    string
	logAllEvaluations bool
	deferEnabled      bool
	clock             Clock
	uuidSource        UUIDSource
}

// Option mutates a referenceConfig. Options compose the same way the
// production code's MediatorOption set does (later option wins).
type Option func(*referenceConfig)

// Clock is the minimal time interface the helper accepts for deterministic
// tests. Production code uses time.Now().UTC().
type Clock interface {
	Now() time.Time
}

// UUIDSource is the minimal UUID generator interface the helper accepts for
// deterministic tests. Production code uses uuid.New.
type UUIDSource func() uuid.UUID

// WithPolicy points the reference mediator at an alternative policy fixture
// on disk. Default is fixtures/policies/reference.yaml resolved relative to
// the calling test file.
func WithPolicy(path string) Option {
	return func(c *referenceConfig) {
		c.policyPath = path
		c.policyBody = nil
	}
}

// WithPolicyBody supplies an in-memory policy document so tests do not need
// to write a temp file just to flip one rule. Mutually exclusive with
// WithPolicy: whichever option is applied last wins.
func WithPolicyBody(body []byte) Option {
	return func(c *referenceConfig) {
		c.policyBody = append([]byte(nil), body...)
		c.policyPath = ""
	}
}

// WithHumanPrincipal overrides the captured HumanPrincipal value, including
// setting it to the empty string (which drives the R6 deny-on-missing path).
func WithHumanPrincipal(value string) Option {
	return func(c *referenceConfig) {
		c.humanPrincipal = value
	}
}

// WithLogAllEvaluations toggles the mediator's LogAllEvaluations switch.
// Defaults to true so allow decisions also produce a receipt row, matching
// the policy.log_all_evaluations CLI default.
func WithLogAllEvaluations(v bool) Option {
	return func(c *referenceConfig) {
		c.logAllEvaluations = v
	}
}

// WithDeferEnabled toggles the deferral configuration's Enabled flag.
// Defaults to true so the defer rule in reference.yaml takes effect.
func WithDeferEnabled(v bool) Option {
	return func(c *referenceConfig) {
		c.deferEnabled = v
	}
}

// WithClock installs a deterministic clock. The helper holds the clock and
// exposes it on the returned Mediator's surrounding ReferenceBundle so
// tests that want byte-stable receipt hashes can pass `RecordedAt` through
// receipt.RecordInput directly.
func WithClock(c Clock) Option {
	return func(cfg *referenceConfig) {
		cfg.clock = c
	}
}

// WithUUIDSource installs a deterministic UUID generator. Exposed on the
// ReferenceBundle for the same reason as WithClock: production code uses
// uuid.New() unconditionally, so byte-stable hash tests opt in by calling
// the receipt generator directly with pre-computed IDs.
func WithUUIDSource(s UUIDSource) Option {
	return func(c *referenceConfig) {
		c.uuidSource = s
	}
}

// ReferenceBundle is the assembled reference mediator + the collaborators
// tests inspect (the store, the receipt generator, the accumulator, the
// signer, and any deterministic Clock / UUIDSource the caller installed).
// Tests that need byte-stable receipt hashes use Receipts + Clock + UUIDs
// directly. Tests that only need to drive the mediator use Mediator.
type ReferenceBundle struct {
	Mediator       *aarm.Mediator
	Policy         *pdp.Policy
	PolicyHash     []byte
	Store          *storage.SQLiteStore
	Receipts       receipt.Generator
	Accumulator    accumulator.Accumulator
	Adapter        mediation.Adapter
	Signer         *receipt.Ed25519Signer
	Verifier       *receipt.Ed25519Verifier
	HumanPrincipal string
	Clock          Clock
	UUIDs          UUIDSource
}

// TestEd25519Seed is the deterministic Ed25519 seed used by the reference
// mediator's receipt signer. NEVER use this in production. The seed is fixed
// so signature bytes over the same input are reproducible across runs.
//
// TEST ONLY.
var TestEd25519Seed = [ed25519.SeedSize]byte{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
	0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
	0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
}

const defaultHumanPrincipal = "conformance-suite"

// NewReferenceMediator assembles a fresh AARM mediator wired up to a fresh
// SQLite store, the reference policy fixture, a deterministic Ed25519
// signer, and the production classifier + injection scorer + identity
// capturer. Each call returns its own store so tests are isolated by
// default.
func NewReferenceMediator(t *testing.T, opts ...Option) *ReferenceBundle {
	t.Helper()

	cfg := &referenceConfig{
		humanPrincipal:    defaultHumanPrincipal,
		logAllEvaluations: true,
		deferEnabled:      true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	var policy *pdp.Policy
	switch {
	case len(cfg.policyBody) > 0:
		p, err := pdp.ParsePolicy(cfg.policyBody)
		require.NoError(t, err, "parse policy body")
		policy = p
	case cfg.policyPath != "":
		p, err := pdp.LoadPolicyFile(cfg.policyPath)
		require.NoError(t, err, "load policy %s", cfg.policyPath)
		policy = p
	default:
		path := defaultReferencePolicyPath(t)
		p, err := pdp.LoadPolicyFile(path)
		require.NoError(t, err, "load default reference policy %s", path)
		policy = p
	}

	store := storagetest.NewStore(t)

	priv := ed25519.NewKeyFromSeed(TestEd25519Seed[:])
	signer, err := receipt.NewEd25519Signer(priv)
	require.NoError(t, err, "build deterministic signer")
	pub, ok := priv.Public().(ed25519.PublicKey)
	require.True(t, ok, "extract public key")
	trust := &receipt.TrustStore{
		Keys: []receipt.TrustStoreEntry{
			{
				KeyID:   signer.KeyID(),
				Pub:     base64Pub(pub),
				Created: time.Unix(0, 0).UTC(),
				Note:    "conformance-suite TEST ONLY",
			},
		},
	}
	verifier, err := receipt.NewEd25519Verifier(trust)
	require.NoError(t, err, "build verifier")

	rg := receipt.NewSQLite(store, receipt.WithSigner(signer))
	acc := accumulator.NewSQLite(store)
	cap := identity.NewStaticCapturer(identity.Capture{
		HumanPrincipal:  cfg.humanPrincipal,
		ServiceIdentity: "",
		RoleScope:       "uid=0",
	})
	adapter := mediation.NewHookAdapter(
		mediation.WithClassifier(classify.NewFailSafe(classify.NewHeuristic(), classify.LabelUnknownSensitive)),
		mediation.WithInjectionScorer(injectscore.NewHeuristic()),
		mediation.WithIdentityCapturer(cap),
	)

	med, err := aarm.NewMediator(policy,
		aarm.WithAccumulator(acc),
		aarm.WithReceiptGenerator(rg),
		aarm.WithAdapter(adapter),
		aarm.WithMediatorConfig(aarm.MediatorConfig{LogAllEvaluations: cfg.logAllEvaluations}),
		aarm.WithDeferralConfig(aarm.DeferralConfig{
			Enabled:               cfg.deferEnabled,
			TimeoutSeconds:        600,
			FreshSessionSeconds:   60,
			ConflictTriggersDefer: true,
		}),
	)
	require.NoError(t, err, "construct reference mediator")

	return &ReferenceBundle{
		Mediator:       med,
		Policy:         policy,
		PolicyHash:     policy.Hash(),
		Store:          store,
		Receipts:       rg,
		Accumulator:    acc,
		Adapter:        adapter,
		Signer:         signer,
		Verifier:       verifier,
		HumanPrincipal: cfg.humanPrincipal,
		Clock:          cfg.clock,
		UUIDs:          cfg.uuidSource,
	}
}

// defaultReferencePolicyPath returns the on-disk path to the suite's
// fixtures/policies/reference.yaml. It walks upward from the calling test
// file's directory looking for the fixtures tree so tests work both when
// invoked via `go test` (cwd is the package dir) and via the bundled
// gryph-conformance.test binary (cwd may be elsewhere).
func defaultReferencePolicyPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"fixtures/policies/reference.yaml",
		"./test/conformance/aarm/fixtures/policies/reference.yaml",
		"../fixtures/policies/reference.yaml",
		"../../fixtures/policies/reference.yaml",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	cwd, _ := os.Getwd()
	require.Fail(t, "could not locate fixtures/policies/reference.yaml", "cwd=%s", cwd)
	return ""
}

// base64Pub renders the trust-store pub field as standard base64.
func base64Pub(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}
