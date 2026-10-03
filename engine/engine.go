// Package engine assembles the Gryph runtime: configuration, storage, the
// agent registry, the redactor, the security evaluator with the policy
// check, and the decision service. The CLI and every other entry point
// build on it, so the hook path and the commands share one wiring.
package engine

import (
	"context"
	"path/filepath"

	aarmsec "github.com/safedep/gryph/aarm"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/agent/codex"
	"github.com/safedep/gryph/agent/commandcode"
	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/agent/devin"
	"github.com/safedep/gryph/agent/gemini"
	"github.com/safedep/gryph/agent/opencode"
	"github.com/safedep/gryph/agent/piagent"
	"github.com/safedep/gryph/agent/windsurf"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/storage"
)

// Runtime holds the dependencies that every command and the hook path share.
type Runtime struct {
	Config   *config.Config
	Store    storage.Store
	Registry *agent.Registry
	Paths    *config.Paths
	Security *security.Evaluator
	Redactor *privacy.Redactor

	exportKey []byte

	// policyCheck holds the lazily-loaded AARM policy check, when the policy
	// layer is enabled. The decision service reaches the Mediator through it
	// for post-hook RecordResult calls.
	policyCheck *lazyPolicyCheck
}

// AarmMediator returns the loaded AARM Mediator, or nil if policy is
// disabled, has not been used yet, or failed to load.
func (a *Runtime) AarmMediator() *aarmsec.Mediator {
	if a == nil || a.policyCheck == nil {
		return nil
	}
	return a.policyCheck.Mediator()
}

// New assembles the runtime for cfg. It opens no store. Call InitStore for
// the database.
func New(cfg *config.Config) (*Runtime, error) {
	return newRuntime(cfg, config.ResolvePaths())
}

// NewPartition assembles the runtime of one account's partition of the
// decision service: every per-user path sits under dir, and the store is
// open. The policy comes from the managed sources, as for every runtime,
// and the user's own policy files are the ones under dir, which the
// service account owns and no agent user can write.
func NewPartition(ctx context.Context, cfg *config.Config, dir string) (*Runtime, error) {
	paths := &config.Paths{
		ConfigFile:   filepath.Join(dir, "config.yml"),
		ConfigDir:    dir,
		DataDir:      dir,
		DatabaseFile: filepath.Join(dir, "audit.db"),
		CacheDir:     filepath.Join(dir, "cache"),
		BackupsDir:   filepath.Join(dir, "backups"),
	}
	rt, err := newRuntime(cfg, paths)
	if err != nil {
		return nil, err
	}
	store, err := storage.NewSQLiteStore(paths.DatabaseFile)
	if err != nil {
		return nil, err
	}
	if err := store.Init(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	rt.Store = store
	return rt, nil
}

func newRuntime(cfg *config.Config, paths *config.Paths) (*Runtime, error) {

	// Merge default patterns with config patterns
	// There may be duplicates, but that's okay for now.
	sensitivePathPatterns := append(privacy.DefaultSensitivePatterns(), cfg.Privacy.SensitivePaths...)
	redactPatterns := append(privacy.DefaultRedactPatterns(), cfg.Privacy.RedactPatterns...)

	// Create shared privacy checker
	privacyChecker, err := privacy.NewRedactor(sensitivePathPatterns, redactPatterns)
	if err != nil {
		return nil, err
	}

	registry := agent.NewRegistry()
	RegisterAdapters(registry, privacyChecker, cfg)

	var rt *Runtime

	policyCfg := cfg.EffectivePolicy()
	failOpen := policyCfg.FailMode == string(aarmsec.FailOpen)
	sec := security.New(&security.Config{FailOpen: failOpen})
	var policyCheck *lazyPolicyCheck
	if policyCfg.Enabled {
		policyCheck = newLazyPolicyCheck(cfg, paths, func() storage.Store {
			if rt == nil {
				return nil
			}
			return rt.Store
		})
		sec.RegisterCheck(policyCheck)
	}

	// Invoke any check factories that external binaries registered during
	// init() via RegisterCheckFactory. Factories that return nil are
	// skipped so callers can conditionally opt out based on config.
	for _, f := range checkFactories {
		if c := f(cfg); c != nil {
			sec.RegisterCheck(c)
		}
	}

	rt = &Runtime{
		Config:      cfg,
		Registry:    registry,
		Paths:       paths,
		Security:    sec,
		Redactor:    privacyChecker,
		policyCheck: policyCheck,
	}
	return rt, nil
}

// ExportProfile returns the named export profile, keyed with the export key
// of the install. It creates the key on first use.
func (a *Runtime) ExportProfile(name string) (privacy.ExportProfile, error) {
	p, err := a.Config.ExportProfile(name)
	if err != nil {
		return privacy.ExportProfile{}, err
	}
	if a.exportKey == nil {
		key, err := config.LoadOrCreateExportKey(a.Config.ExportKeyFile())
		if err != nil {
			return privacy.ExportProfile{}, err
		}
		a.exportKey = key
	}
	return p.WithDigestKey(a.exportKey), nil
}

// DecisionService returns the in-process decision service for hook events.
// Call it after InitStore.
func (a *Runtime) DecisionService() decision.Service {
	return decision.NewLocal(a.Store, a.Security, a.Redactor, a.Config.GetAgentLoggingLevel,
		decision.WithResultRecorder(func() decision.ResultRecorder {
			if m := a.AarmMediator(); m != nil {
				return m
			}
			return nil
		}),
		decision.WithHookSpecs(a.Registry.HookSpec),
		decision.WithClassifier(a.classifier()),
		decision.WithHookErrorRecorder(func(ctx context.Context, agentName string, details map[string]any, errorMessage string) error {
			return LogSelfAudit(ctx, a.Store, SelfAuditActionHookError, agentName, details, SelfAuditResultError, errorMessage)
		}),
	)
}

// classifier returns the content classifier, or nil when classification is
// off. A nil *classify.Heuristic in the interface would not be nil, so this
// returns the interface.
func (a *Runtime) classifier() decision.Classifier {
	if h := NewClassifier(a.Config); h != nil {
		return h
	}
	return nil
}

// InitStore initializes the database store.
func (a *Runtime) InitStore(ctx context.Context) error {
	dbPath := a.Config.GetDatabasePath()
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		return err
	}
	if err := store.Init(ctx); err != nil {
		return err
	}
	a.Store = store
	a.recordPathMigration(ctx)
	return nil
}

// recordPathMigration writes the self-audit entry for a completed layout
// migration. The migration runs in the config package before any store
// exists, so the record arrives through a marker file that the first
// store-opening command consumes.
func (a *Runtime) recordPathMigration(ctx context.Context) {
	record := config.ConsumeMigrationMarker()
	if record == nil {
		return
	}

	details := map[string]interface{}{"moves": record.Moves}
	if len(record.Warnings) > 0 {
		details["warnings"] = record.Warnings
	}

	if err := LogSelfAudit(ctx, a.Store, SelfAuditActionPathMigration, "", details,
		SelfAuditResultSuccess, ""); err != nil {
		log.Warnf("failed to log path migration self-audit: %v", err)
	}
}

// Close closes the application resources.
func (a *Runtime) Close() error {
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}

// checkFactories is the registry of external security-check factories.
// External binaries composing gryph as a library call RegisterCheckFactory
// during init() to add Checks that New will register on the security
// evaluator alongside the built-in placeholder check.
var checkFactories []func(cfg *config.Config) security.Check

// RegisterCheckFactory registers a factory that produces a security.Check.
// Intended for external binaries that import gryph as a library and want to
// contribute additional security checks without modifying this package.
//
// Call order: typically from init() in a package blank-imported by the
// external binary's main. Each registered factory is invoked once per
// New() call, and the returned Check (if non-nil) is added to the
// evaluator. Factories that return nil are ignored — this lets callers
// conditionally enable/disable checks based on the provided *config.Config.
//
// This function is not safe for concurrent use. Register factories before
// any goroutine calls New.
func RegisterCheckFactory(f func(cfg *config.Config) security.Check) {
	if f == nil {
		return
	}
	checkFactories = append(checkFactories, f)
}

// RegisterAdapters registers every supported agent adapter. It is the single
// list of adapters, so the self-protection globs come from the same source.
func RegisterAdapters(registry *agent.Registry, privacyChecker *privacy.Redactor, cfg *config.Config) {
	claudecode.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentClaudeCode), cfg.Logging.ContentHash)
	cursor.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentCursor), cfg.Logging.ContentHash)
	gemini.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentGemini), cfg.Logging.ContentHash)
	opencode.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentOpenCode), cfg.Logging.ContentHash)
	windsurf.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentWindsurf), cfg.Logging.ContentHash)
	piagent.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentPiAgent), cfg.Logging.ContentHash)
	codex.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentCodex), cfg.Logging.ContentHash)
	devin.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentDevin), cfg.Logging.ContentHash)
	commandcode.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentCommandCode), cfg.Logging.ContentHash)

	// For now, let us keep openclaw agent disabled because it is non-functional
	// openclaw.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentOpenClaw), cfg.Logging.ContentHash)
}

// HookConfigGlobs returns the hook config globs of every supported adapter.
// It does not need a loaded Runtime, so `gryph policy list` works when the app
// fails to load.
func HookConfigGlobs() []string {
	registry := agent.NewRegistry()
	RegisterAdapters(registry, nil, config.Default())
	return registry.HookConfigGlobs()
}
