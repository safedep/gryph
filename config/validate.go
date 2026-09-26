package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/safedep/gryph/aarm/model"
)

// validate checks the configuration for errors.
func validate(cfg *Config) error {
	// Validate logging level
	if !isValidLoggingLevel(cfg.Logging.Level) {
		return fmt.Errorf("invalid logging level: %s (must be minimal, standard, or full)", cfg.Logging.Level)
	}

	// Validate truncation limits
	if cfg.Logging.StdoutMaxChars < 0 {
		return fmt.Errorf("logging.stdout_max_chars must be non-negative")
	}
	if cfg.Logging.StderrMaxChars < 0 {
		return fmt.Errorf("logging.stderr_max_chars must be non-negative")
	}
	if cfg.Logging.ContextMaxChars < 0 {
		return fmt.Errorf("logging.context_max_chars must be non-negative")
	}

	// Validate retention days
	if cfg.Storage.RetentionDays < 0 {
		return fmt.Errorf("storage.retention_days must be non-negative")
	}

	// Validate redact patterns are valid regex
	for i, pattern := range cfg.Privacy.RedactPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid redact pattern at index %d: %s", i, err)
		}
	}

	// Validate color mode
	if !isValidColorMode(cfg.Display.Colors) {
		return fmt.Errorf("invalid display.colors: %s (must be auto, always, or never)", cfg.Display.Colors)
	}

	// Validate timezone mode
	if !isValidTimezoneMode(cfg.Display.Timezone) {
		return fmt.Errorf("invalid display.timezone: %s (must be local or utc)", cfg.Display.Timezone)
	}

	// Validate stream targets
	if err := validateStreamTargets(cfg.Streams.Targets); err != nil {
		return err
	}

	if err := validatePolicyConfig(cfg.Policy); err != nil {
		return err
	}

	// Validate agent logging levels if set
	for name, ac := range cfg.Agents {
		if ac.LoggingLevel != "" && !isValidLoggingLevel(ac.LoggingLevel) {
			return fmt.Errorf("invalid agents.%s.logging_level: %s", name, ac.LoggingLevel)
		}
	}

	return nil
}

// isValidLoggingLevel returns true if the given level is valid.
func isValidLoggingLevel(level LoggingLevel) bool {
	switch level {
	case LoggingMinimal, LoggingStandard, LoggingFull:
		return true
	default:
		return false
	}
}

// isValidColorMode returns true if the given mode is valid.
func isValidColorMode(mode ColorMode) bool {
	switch mode {
	case ColorAuto, ColorAlways, ColorNever:
		return true
	default:
		return false
	}
}

// isValidTimezoneMode returns true if the given mode is valid.
func isValidTimezoneMode(mode TimezoneMode) bool {
	switch mode {
	case TimezoneLocal, TimezoneUTC:
		return true
	default:
		return false
	}
}

// knownStreamTargetTypes lists the valid stream target types.
var knownStreamTargetTypes = map[string]bool{
	streamTargetTypeStdout: true,
	streamTargetTypeNop:    true,
}

func validatePolicyConfig(cfg PolicyConfig) error {
	if !cfg.Enabled {
		return nil
	}
	switch model.FailMode(cfg.FailMode) {
	case model.FailOpen, model.FailClosed:
	default:
		return fmt.Errorf("invalid policy.fail_mode: %q (must be %s or %s)", cfg.FailMode, model.FailOpen, model.FailClosed)
	}
	if cfg.ContextRetentionDays < 0 {
		return fmt.Errorf("policy.context_retention_days must be non-negative")
	}
	if cfg.ReceiptRetentionDays < 0 {
		return fmt.Errorf("policy.receipt_retention_days must be non-negative")
	}
	switch cfg.Approval.Mode {
	case "", ApprovalModeNop, ApprovalModeCLI:
	default:
		return fmt.Errorf("invalid policy.approval.mode: %q (must be %s or %s)", cfg.Approval.Mode, ApprovalModeCLI, ApprovalModeNop)
	}
	if cfg.Approval.TimeoutSeconds < 1 {
		return fmt.Errorf("policy.approval.timeout_seconds must be >= 1")
	}
	mode := cfg.Receipts.EffectiveSignMode()
	switch mode {
	case SignModeAuto, SignModeAlways, SignModeNever:
	default:
		return fmt.Errorf("invalid policy.receipts.sign_mode: %q (must be %s, %s, or %s)", cfg.Receipts.SignMode, SignModeAuto, SignModeAlways, SignModeNever)
	}
	if mode == SignModeAlways {
		keyPath := cfg.Receipts.KeyPath
		if keyPath == "" {
			keyPath = DefaultReceiptKeyPath(nil)
		}
		info, err := os.Stat(keyPath)
		if err != nil {
			return fmt.Errorf("policy.receipts.sign_mode=always but key file %s is unreadable: %w", keyPath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("policy.receipts.sign_mode=always but key path %s is a directory", keyPath)
		}
	}
	if err := validatePolicyDeferConfig(cfg.Defer); err != nil {
		return err
	}
	validatePolicyIdentityConfig(cfg.Identity)
	return nil
}

// validatePolicyIdentityConfig is a structural no-op today: every combination
// of (Enabled, RequireHumanPrincipal) is legal. When enabled=false the
// require_human_principal switch is silently a no-op at the mediator (we
// cannot enforce identity we did not capture). Kept as a hook so future
// validation (e.g. forbidden providers, well-formed values) lands in one
// place.
func validatePolicyIdentityConfig(_ IdentityConfig) {}

func validatePolicyDeferConfig(cfg DeferConfig) error {
	if cfg.FreshSessionSeconds < 0 {
		return fmt.Errorf("policy.defer.fresh_session_seconds must be non-negative")
	}
	if cfg.TimeoutSeconds < 0 {
		return fmt.Errorf("policy.defer.timeout_seconds must be non-negative")
	}
	if cfg.AutoResolveOnTimeout != "" && cfg.AutoResolveOnTimeout != DeferAutoResolveDeny {
		return fmt.Errorf("policy.defer.auto_resolve_on_timeout must be %q (AARM R4 forbids implicit allow on timeout)", DeferAutoResolveDeny)
	}
	return nil
}

func validateStreamTargets(targets []StreamTargetConfig) error {
	names := make(map[string]bool, len(targets))
	for i, t := range targets {
		if t.Name == "" {
			return fmt.Errorf("streams.targets[%d]: name must not be empty", i)
		}
		if t.Type == "" {
			return fmt.Errorf("streams.targets[%d]: type must not be empty", i)
		}
		if !knownStreamTargetTypes[t.Type] {
			return fmt.Errorf("streams.targets[%d]: unknown type %q", i, t.Type)
		}
		if names[t.Name] {
			return fmt.Errorf("streams.targets[%d]: duplicate name %q", i, t.Name)
		}
		names[t.Name] = true
	}
	return nil
}
