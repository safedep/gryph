package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	aarmsec "github.com/safedep/gryph/aarm"
	coresecurity "github.com/safedep/gryph/core/security"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/classify"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/injectscore"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/storage"
)

func LoadPolicyMediator(cfg *config.Config, paths *config.Paths, store storage.Store) (*aarmsec.Mediator, error) {
	return loadPolicyMediator(cfg, paths, store, BuildPolicyLoader(cfg, paths))
}

// loadPolicyMediator builds the Mediator for the policy that ldr loads.
// extra options run after the ones the config sets.
func loadPolicyMediator(cfg *config.Config, paths *config.Paths, store storage.Store, ldr *loader.Loader, extra ...aarmsec.MediatorOption) (*aarmsec.Mediator, error) {
	policy, err := ldr.Load(context.Background())
	if err != nil {
		return nil, err
	}
	var opts []aarmsec.MediatorOption
	if store != nil {
		opts = append(opts, aarmsec.WithAccumulator(accumulator.NewSQLite(store)))
		if cfg != nil {
			opts = append(opts, aarmsec.WithCELEntries(cfg.Policy.Context.CELEntries))
		}
		gen, err := newReceiptGenerator(cfg, paths, store)
		if err != nil {
			return nil, err
		}
		opts = append(opts, aarmsec.WithReceiptGenerator(gen))
	}
	if cfg != nil {
		policyCfg := cfg.EffectivePolicy()
		opts = append(opts, aarmsec.WithMediatorConfig(aarmsec.MediatorConfig{
			LogAllEvaluations: policyCfg.LogAllEvaluations,
			ApprovalTimeout:   time.Duration(policyCfg.Approval.TimeoutSeconds) * time.Second,
		}))

		var classifier classify.Classifier
		if h := NewClassifier(cfg); h != nil {
			classifier = h
		}
		if !policyCfg.Classify.FailOpen {
			classifier = classify.NewFailSafe(classifier, privacy.ClassUnknownSensitive)
		}

		var adapterOpts []mediation.CommonOption
		if classifier != nil {
			adapterOpts = append(adapterOpts, mediation.WithClassifier(classifier))
		}

		if policyCfg.InjectionScore.Enabled {
			adapterOpts = append(adapterOpts, mediation.WithInjectionScorer(injectscore.NewHeuristic()))
		}

		var identityCapturer identity.Capturer
		if policyCfg.Identity.Enabled {
			identityCapturer = identity.NewDefaultCapturer()
		} else {
			identityCapturer = identity.NewStaticCapturer(identity.Capture{})
		}
		adapterOpts = append(adapterOpts, mediation.WithIdentityCapturer(identityCapturer))
		adapterOpts = append(adapterOpts, mediation.WithShellBudget(policyCfg.ShellBudget))

		opts = append(opts, aarmsec.WithAdapter(mediation.NewHookAdapter(adapterOpts...)))
		opts = append(opts, aarmsec.WithContentStrip(func(e *events.Event) bool {
			return decision.StripsContent(e, cfg.GetAgentLoggingLevel(e.AgentName))
		}))
		opts = append(opts, aarmsec.WithIdentityConfig(aarmsec.IdentityConfig{
			Enabled:               policyCfg.Identity.Enabled,
			RequireHumanPrincipal: policyCfg.Identity.RequireHumanPrincipal,
		}))
		if store != nil {
			opts = append(opts, aarmsec.WithIdentityAuditHook(newIdentityAuditHook(store)))
		}

		switch policyCfg.Approval.Mode {
		case config.ApprovalModeCLI:
			opts = append(opts, aarmsec.WithApprovalService(approval.NewCLIPrompt(
				approval.WithRequireNote(policyCfg.Approval.RequireNote),
			)))
		default:
			opts = append(opts, aarmsec.WithApprovalService(approval.NewNop()))
		}

		if store != nil {
			opts = append(opts, aarmsec.WithApprovalAuditHook(newApprovalAuditHook(store)))
		}

		opts = append(opts, aarmsec.WithDeferralConfig(aarmsec.DeferralConfig{
			Enabled:               policyCfg.Defer.Enabled,
			TimeoutSeconds:        policyCfg.Defer.TimeoutSeconds,
			FreshSessionSeconds:   policyCfg.Defer.FreshSessionSeconds,
			ConflictTriggersDefer: policyCfg.Defer.ConflictTriggersDefer,
		}))
		if store != nil {
			opts = append(opts, aarmsec.WithDeferralHook(newDeferralHook(store)))
		}
	}
	opts = append(opts, extra...)
	return aarmsec.NewMediator(policy, opts...)
}

// newReceiptGenerator returns the receipt generator of this install: the
// SQLite generator, signed when the config asks for it, with the self-audit
// row per signed receipt.
func newReceiptGenerator(cfg *config.Config, paths *config.Paths, store storage.Store) (receipt.Generator, error) {
	var recOpts []receipt.GeneratorOption
	if cfg != nil && cfg.Policy.Receipts.EffectiveSignMode() != config.SignModeNever {
		signer, err := LoadReceiptSignerFromConfig(cfg, paths)
		if err != nil {
			return nil, fmt.Errorf("load receipt signer: %w", err)
		}
		if signer != nil {
			recOpts = append(recOpts, receipt.WithSigner(signer))
		}
	}
	return newAuditingReceiptGenerator(receipt.NewSQLite(store, recOpts...), store), nil
}

// auditingReceiptGenerator wraps a receipt.Generator and emits a
// receipt_signed self-audit row after each signed receipt insert completes.
// The audit is emitted after the receipt's write transaction has committed
// so the audit insert does not contend with the receipt insert for the
// SQLite writer lock.
type auditingReceiptGenerator struct {
	inner receipt.Generator
	store storage.Store
}

func newAuditingReceiptGenerator(inner receipt.Generator, store storage.Store) *auditingReceiptGenerator {
	return &auditingReceiptGenerator{inner: inner, store: store}
}

func (a *auditingReceiptGenerator) Record(ctx context.Context, in *receipt.RecordInput) (*receipt.Record, error) {
	rec, err := a.inner.Record(ctx, in)
	if err != nil {
		return rec, err
	}
	if rec != nil && rec.SignerKeyID != "" && a.store != nil {
		details := map[string]interface{}{
			"key_id":     rec.SignerKeyID,
			"session_id": in.SessionID.String(),
			"sequence":   rec.Sequence,
		}
		if logErr := LogSelfAudit(ctx, a.store, SelfAuditActionReceiptSigned, "",
			details, SelfAuditResultSuccess, ""); logErr != nil {
			log.Errorf("failed to record receipt_signed audit: %v", logErr)
		}
	}
	return rec, nil
}

func (a *auditingReceiptGenerator) UpdateResult(ctx context.Context, sessionID uuid.UUID, sequence int64, result model.Result) error {
	return a.inner.UpdateResult(ctx, sessionID, sequence, result)
}

func (a *auditingReceiptGenerator) UpdateDecision(ctx context.Context, sessionID uuid.UUID, sequence int64, decision string, resultStatus string, note string) error {
	return a.inner.UpdateDecision(ctx, sessionID, sequence, decision, resultStatus, note)
}

func (a *auditingReceiptGenerator) UpdateApproval(ctx context.Context, sessionID uuid.UUID, sequence int64, approval map[string]any) error {
	return a.inner.UpdateApproval(ctx, sessionID, sequence, approval)
}

// newDeferralHook returns the Mediator DeferralHook that persists the
// pending-deferral row, emits the deferral_requested self-audit row, and
// renders the CLI-shaped operator hint the Mediator splices into the
// agent-facing block message. Keeping the hint here (instead of inside
// aarm) lets aarm stay decoupled from CLI command spellings.
func newDeferralHook(store storage.Store) aarmsec.DeferralHook {
	return func(ctx context.Context, r aarmsec.DeferralRecord) (uuid.UUID, string, error) {
		if store == nil {
			return uuid.Nil, "", nil
		}
		row := &storage.DeferredActionRow{
			ID:              uuid.New(),
			SessionID:       r.SessionID,
			ReceiptSequence: r.ReceiptSequence,
			ActionID:        r.ActionID,
			DeferredAt:      r.DeferredAt,
			ExpiresAt:       r.ExpiresAt,
			Reason:          r.Reason,
			Status:          storage.DeferredActionStatusPending,
		}
		if err := store.InsertDeferredAction(ctx, row); err != nil {
			log.Errorf("failed to insert deferred action: %v", err)
			return uuid.Nil, "", err
		}
		details := map[string]interface{}{
			"session_id":         r.SessionID.String(),
			"action_id":          r.ActionID.String(),
			"receipt_sequence":   r.ReceiptSequence,
			"deferred_action_id": row.ID.String(),
			"reason":             r.Reason,
			"expires_at":         r.ExpiresAt.Format(time.RFC3339),
		}
		agent := ""
		if r.Action != nil {
			agent = r.Action.Agent
			details["agent"] = r.Action.Agent
			details["tool"] = r.Action.Tool
			details["action_type"] = string(r.Action.Type)
		}
		if r.Decision != nil {
			details["matched_rule_ids"] = r.Decision.MatchedRuleIDs
		}
		if err := LogSelfAudit(ctx, store, SelfAuditActionDeferralRequested, agent,
			details, SelfAuditResultSuccess, ""); err != nil {
			log.Errorf("failed to record deferral_requested audit: %v", err)
		}
		hint := fmt.Sprintf("Resolve with `gryph policy deferrals resolve --id %s`.",
			shortDeferralID(row.ID))
		return row.ID, hint, nil
	}
}

// shortDeferralID renders the 8-character prefix used in operator hints.
// Falls back to the full string for IDs that are somehow shorter so the
// hint always references something the operator can act on.
func shortDeferralID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

func newApprovalAuditHook(store storage.Store) aarmsec.ApprovalAuditHook {
	return func(ctx context.Context, e aarmsec.ApprovalAudit) {
		if store == nil {
			return
		}
		details := map[string]interface{}{}
		agentName := ""
		if e.Request != nil {
			details["session_id"] = e.Request.SessionID.String()
			details["action_id"] = e.Request.ActionID.String()
			if e.Request.Action != nil {
				agentName = e.Request.Action.Agent
				details["agent"] = e.Request.Action.Agent
				details["tool"] = e.Request.Action.Tool
				details["action_type"] = string(e.Request.Action.Type)
			}
		}
		if e.Decision != nil {
			details["matched_rule_ids"] = e.Decision.MatchedRuleIDs
		}
		if e.Outcome != nil {
			details["approver"] = e.Outcome.Approver
			if e.Outcome.Note != "" {
				details["note"] = e.Outcome.Note
			}
			for k, v := range e.Outcome.Meta() {
				if k != "approver" && v != "" {
					details[k] = v
				}
			}
		}
		result := SelfAuditResultSuccess
		errMsg := ""
		if e.Error != nil {
			result = SelfAuditResultError
			errMsg = e.Error.Error()
		}
		if e.Action == SelfAuditActionApprovalDenied || e.Action == SelfAuditActionApprovalTimeout || e.Action == SelfAuditActionApprovalPending {
			result = SelfAuditResultSkipped
		}
		if err := LogSelfAudit(ctx, store, e.Action, agentName, details, result, errMsg); err != nil {
			log.Errorf("failed to record %s audit: %v", e.Action, err)
		}
	}
}

// newIdentityAuditHook returns a Mediator IdentityAuditHook that emits the
// identity_missing self-audit row on the pre-PDP block path.
func newIdentityAuditHook(store storage.Store) aarmsec.IdentityAuditHook {
	return func(ctx context.Context, e aarmsec.IdentityAudit) {
		if store == nil {
			return
		}
		details := map[string]interface{}{}
		agentName := ""
		if e.Action != nil {
			agentName = e.Action.Agent
			details["session_id"] = e.Action.SessionID.String()
			details["action_id"] = e.Action.ID.String()
			details["agent"] = e.Action.Agent
			details["tool"] = e.Action.Tool
			details["action_type"] = string(e.Action.Type)
		}
		errMsg := ""
		if e.Decision != nil {
			errMsg = e.Decision.Reason
		}
		if err := LogSelfAudit(ctx, store, SelfAuditActionIdentityMissing, agentName,
			details, SelfAuditResultError, errMsg); err != nil {
			log.Errorf("failed to record identity_missing audit: %v", err)
		}
	}
}

// lazyPolicyCheck defers policy load until the first hook event so a broken
// policy file does not lock the user out of `gryph policy validate/test`,
// the very commands they need to diagnose and fix it. Load errors propagate
// to the security evaluator, which applies policy.fail_mode. The first load
// failure is also recorded in the self-audit log so it surfaces under
// `gryph self-log` even when fail_mode=open silently allows the action.
type lazyPolicyCheck struct {
	cfg      *config.Config
	paths    *config.Paths
	getStore func() storage.Store
	// extra are options the runtime's owner adds before the first load,
	// after the ones the configuration sets.
	extra []aarmsec.MediatorOption

	once sync.Once
	med  *aarmsec.Mediator
	err  error
}

func newLazyPolicyCheck(cfg *config.Config, paths *config.Paths, getStore func() storage.Store) *lazyPolicyCheck {
	return &lazyPolicyCheck{cfg: cfg, paths: paths, getStore: getStore}
}

func (l *lazyPolicyCheck) load() (*aarmsec.Mediator, error) {
	l.once.Do(func() {
		var store storage.Store
		if l.getStore != nil {
			store = l.getStore()
		}
		l.med, l.err = loadPolicyMediator(l.cfg, l.paths, store, BuildPolicyLoader(l.cfg, l.paths), l.extra...)
		if l.err != nil {
			l.recordLoadFailure(l.err)
		}
	})
	return l.med, l.err
}

func (l *lazyPolicyCheck) recordLoadFailure(loadErr error) {
	if l.getStore == nil {
		return
	}
	store := l.getStore()
	if store == nil {
		return
	}
	details := map[string]interface{}{
		"fail_mode": l.cfg.EffectivePolicy().FailMode,
	}
	if err := LogSelfAudit(context.Background(), store, SelfAuditActionPolicyLoadError, "",
		details, SelfAuditResultError, loadErr.Error()); err != nil {
		log.Errorf("failed to record policy load failure: %v", err)
	}
}

// Mediator returns the underlying aarm Mediator if it has been loaded
// successfully. Returns nil if the policy has not yet been loaded or load
// failed. Used by cli/hook.go to drive post-hook RecordResult calls.
func (l *lazyPolicyCheck) Mediator() *aarmsec.Mediator {
	if l == nil {
		return nil
	}
	return l.med
}

func (l *lazyPolicyCheck) Name() string { return aarmsec.CheckName }

func (l *lazyPolicyCheck) Enabled() bool {
	if l == nil || l.cfg == nil {
		return false
	}
	return l.cfg.EffectivePolicy().Enabled
}

func (l *lazyPolicyCheck) Check(ctx context.Context, event *events.Event, sess *session.Session) (*coresecurity.CheckResult, error) {
	med, err := l.load()
	if err != nil {
		return nil, fmt.Errorf("policy load failed: %w", err)
	}
	result, checkErr := med.Check(ctx, event, sess)
	if checkErr != nil {
		if errors.Is(checkErr, accumulator.ErrSnapshot) {
			l.recordAarmFailure(event, SelfAuditActionContextSnapshotError, "accumulator snapshot", checkErr)
		}
		if errors.Is(checkErr, accumulator.ErrAppend) {
			l.recordAarmFailure(event, SelfAuditActionContextAppendError, "accumulator append", checkErr)
		}
		if errors.Is(checkErr, receipt.ErrInsert) {
			l.recordAarmFailure(event, SelfAuditActionReceiptInsertError, "receipt insert", checkErr)
		}
	}
	return result, checkErr
}

// recordAarmFailure is the shared shape behind every AARM-component self-audit
// emission. label is the short human string used in the fallback log line
// when no store is available. action selects the SelfAudit action constant.
func (l *lazyPolicyCheck) recordAarmFailure(event *events.Event, action, label string, recErr error) {
	if l.getStore == nil {
		log.Warnf("aarm: %s failure (no store): %v", label, recErr)
		return
	}
	store := l.getStore()
	if store == nil {
		log.Warnf("aarm: %s failure (nil store): %v", label, recErr)
		return
	}
	details := map[string]interface{}{}
	agentName := ""
	if event != nil {
		details["session_id"] = event.SessionID.String()
		details["agent"] = event.AgentName
		agentName = event.AgentName
	}
	details["error"] = recErr.Error()
	if err := LogSelfAudit(context.Background(), store, action, agentName,
		details, SelfAuditResultError, recErr.Error()); err != nil {
		log.Errorf("failed to record %s failure: %v", label, err)
	}
}

var _ coresecurity.Check = (*lazyPolicyCheck)(nil)

func BuildPolicyLoader(cfg *config.Config, paths *config.Paths) *loader.Loader {
	return loader.New(policyLoaderSources(cfg, paths)...)
}

// policyLoaderSources returns the ordered policy sources: the managed file
// and directory, the user's global file and policies directory, and the
// built-in source. It is the single definition of the resolution order
// shared by the loader and the inspection commands.
func policyLoaderSources(cfg *config.Config, paths *config.Paths) []loader.Source {
	return policySources(cfg, paths, config.ManagedPolicyState())
}

// policySources assembles the sources for one host state. The managed
// sources load whenever the platform has a managed location, each file
// behind the trust check. The user sources load unless a managed config in
// force sets allow_user_policy to false.
func policySources(cfg *config.Config, paths *config.Paths, managed config.ManagedPolicy) []loader.Source {
	if paths == nil {
		paths = config.ResolvePaths()
	}
	sources := managedSources(managed)
	if UserPolicyAllowed(cfg, managed) {
		sources = append(sources,
			loader.NewOptionalFileSource(config.DefaultPolicyFilePath(paths)),
			loader.NewOptionalDirSource(config.DefaultPolicyDirPath(paths)),
		)
	}
	return AppendBuiltinSource(sources, cfg, paths)
}

// managedSources returns the managed file and directory sources, each
// behind the trust check, or nothing on a platform with no managed
// location.
func managedSources(managed config.ManagedPolicy) []loader.Source {
	if managed.Dir == "" {
		return nil
	}
	return []loader.Source{
		loader.NewManagedFileSource(managed.File, managed.Trust),
		loader.NewManagedDirSource(managed.Dir, managed.Trust),
	}
}

// UserPolicyAllowed reports whether the user's own policy sources load. A
// managed config in force drops them with allow_user_policy: false.
func UserPolicyAllowed(cfg *config.Config, managed config.ManagedPolicy) bool {
	return !managed.Active || cfg == nil || cfg.Policy.AllowUserPolicy
}

// AppendBuiltinSource adds the self-protection source when it is enabled.
func AppendBuiltinSource(sources []loader.Source, cfg *config.Config, paths *config.Paths) []loader.Source {
	if SelfProtectionEnabled(cfg) {
		sources = append(sources, SelfProtectionSource(cfg, paths))
	}
	return sources
}

// SelfProtectionEnabled reports whether the built-in rules load. A managed
// configuration in force keeps them on whatever the file says, because the
// administrator's policy must not be removable from the same file a user
// cannot change anyway.
func SelfProtectionEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if config.ManagedConfigActive() {
		return true
	}
	return cfg.EffectivePolicy().SelfProtection.Enabled
}

// NewClassifier builds the heuristic classifier from the config. It returns
// nil when classification is off. The mediator and the decision service
// share it.
func NewClassifier(cfg *config.Config) *classify.Heuristic {
	if cfg == nil {
		return nil
	}
	policyCfg := cfg.EffectivePolicy()
	if !policyCfg.Classify.Enabled {
		return nil
	}
	secretPaths := cfg.Privacy.SensitivePaths
	if len(secretPaths) == 0 {
		secretPaths = privacy.DefaultSensitivePatterns()
	}
	opts := []classify.HeuristicOption{classify.WithSecretPaths(secretPaths)}
	if len(policyCfg.Classify.ExtraPatterns) > 0 {
		extra := make(map[privacy.Class][]string, len(policyCfg.Classify.ExtraPatterns))
		for class, patterns := range policyCfg.Classify.ExtraPatterns {
			extra[privacy.Class(class)] = patterns
		}
		opts = append(opts, classify.WithExtraPatterns(extra))
	}
	return classify.NewHeuristic(opts...)
}

func SelfProtectionSource(cfg *config.Config, paths *config.Paths) *loader.BuiltinSource {
	return loader.NewBuiltinSource(SelfProtectionGlobs(cfg, paths)...).
		WithReadGlobs(SelfProtectionReadGlobs(cfg, paths)...)
}

// SelfProtectionReadGlobs returns the paths an agent must not read: the
// database with its SQLite side files, the receipt signing key and the
// export key. An agent may read the policy files and the hook configs, so
// they are not here.
func SelfProtectionReadGlobs(cfg *config.Config, paths *config.Paths) []string {
	if cfg == nil {
		return nil
	}
	return append(databaseGlobs(cfg), keyGlobs(cfg, paths)...)
}

// keyGlobs returns the secret keys of the install. An agent that reads the
// receipt key can sign receipts. An agent that reads the export key, or
// writes a known one, can reverse the keyed digests of an export with a
// dictionary.
func keyGlobs(cfg *config.Config, paths *config.Paths) []string {
	globs := []string{filepath.ToSlash(cfg.ResolveReceiptKeyPath(paths))}
	if cfg.GetDatabasePath() != "" {
		globs = append(globs, filepath.ToSlash(cfg.ExportKeyFile()))
	}
	return globs
}

// databaseGlobs returns the database path and its SQLite side files. The
// WAL file holds committed rows until a checkpoint copies them.
func databaseGlobs(cfg *config.Config) []string {
	db := cfg.GetDatabasePath()
	if db == "" {
		return nil
	}
	db = filepath.ToSlash(db)
	return []string{db, db + "-wal", db + "-shm", db + "-journal"}
}

// SelfProtectionGlobs returns the paths an agent must not change: the
// config directory, the database, the keys, the trust store, the gryph
// binary that the hooks run, and every agent's hook config.
func SelfProtectionGlobs(cfg *config.Config, paths *config.Paths) []string {
	var globs []string
	if paths != nil && paths.ConfigDir != "" {
		globs = append(globs, filepath.ToSlash(paths.ConfigDir)+"/**")
	}
	if managed := config.ManagedConfigDir(); managed != "" {
		globs = append(globs, filepath.ToSlash(managed)+"/**")
	}
	if binary := gryphBinaryPath(); binary != "" {
		globs = append(globs, filepath.ToSlash(binary))
	}
	if cfg != nil {
		globs = append(globs, databaseGlobs(cfg)...)
		globs = append(globs, keyGlobs(cfg, paths)...)
		for _, p := range cfg.ReceiptTrustStorePaths(paths) {
			globs = append(globs, filepath.ToSlash(p))
		}
	}
	globs = append(globs, HookConfigGlobs()...)
	return globs
}
