package config

import (
	"fmt"
	"regexp"
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

	// Validate agent logging levels if set
	if cfg.Agents.ClaudeCode.LoggingLevel != "" && !isValidLoggingLevel(cfg.Agents.ClaudeCode.LoggingLevel) {
		return fmt.Errorf("invalid agents.claude-code.logging_level: %s", cfg.Agents.ClaudeCode.LoggingLevel)
	}
	if cfg.Agents.Cursor.LoggingLevel != "" && !isValidLoggingLevel(cfg.Agents.Cursor.LoggingLevel) {
		return fmt.Errorf("invalid agents.cursor.logging_level: %s", cfg.Agents.Cursor.LoggingLevel)
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
