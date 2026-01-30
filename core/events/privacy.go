package events

import (
	"path/filepath"
	"regexp"
	"strings"
)

// PrivacyChecker determines if paths are sensitive and handles content redaction.
type PrivacyChecker struct {
	sensitivePatterns []string
	redactPatterns    []*regexp.Regexp
}

// NewPrivacyChecker creates a new PrivacyChecker with the given patterns.
func NewPrivacyChecker(sensitivePatterns []string, redactPatterns []string) (*PrivacyChecker, error) {
	compiled := make([]*regexp.Regexp, 0, len(redactPatterns))
	for _, pattern := range redactPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}

		compiled = append(compiled, re)
	}

	return &PrivacyChecker{
		sensitivePatterns: sensitivePatterns,
		redactPatterns:    compiled,
	}, nil
}

// DefaultSensitivePatterns returns the default list of sensitive path patterns.
func DefaultSensitivePatterns() []string {
	return []string{
		"**/.env",
		"**/.env.*",
		"**/.env.local",
		"**/secrets/**",
		"**/*.pem",
		"**/*.key",
		"**/*.p12",
		"**/*password*",
		"**/*secret*",
		"**/*credential*",
		"**/.git/config",
		"**/.ssh/**",
		"**/.aws/**",
		"**/.npmrc",
		"**/.pypirc",
	}
}

// DefaultRedactPatterns returns the default list of redaction regex patterns.
func DefaultRedactPatterns() []string {
	return []string{
		`(?i)password[=:]\S+`,
		`(?i)api[_-]?key[=:]\S+`,
		`(?i)token[=:]\S+`,
		`(?i)secret[=:]\S+`,
		`(?i)bearer\s+\S+`,
		`(?i)aws_access_key_id[=:]\S+`,
		`(?i)aws_secret_access_key[=:]\S+`,
	}
}

// IsSensitivePath checks if the given path matches any sensitive pattern.
func (p *PrivacyChecker) IsSensitivePath(path string) bool {
	// Normalize path separators
	normalizedPath := filepath.ToSlash(path)

	for _, pattern := range p.sensitivePatterns {
		if matchGlob(pattern, normalizedPath) {
			return true
		}
	}

	return false
}

// Redact applies redaction patterns to the content, replacing matches with [REDACTED].
func (p *PrivacyChecker) Redact(content string) string {
	result := content
	for _, re := range p.redactPatterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}

	return result
}

// matchGlob performs a simple glob pattern match.
// Supports ** for any path segment and * for any characters within a segment.
func matchGlob(pattern, path string) bool {
	// Handle ** patterns by converting to regex-like matching
	pattern = filepath.ToSlash(pattern)

	// Check for exact match with filename patterns like **/.env
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		// Check if path ends with this suffix
		if strings.HasSuffix(path, "/"+suffix) || path == suffix {
			return true
		}
		// Check if basename matches
		base := filepath.Base(path)
		if matchSimple(suffix, base) {
			return true
		}
		// Check for directory patterns like **/secrets/**
		if strings.HasSuffix(suffix, "/**") {
			dirPart := suffix[:len(suffix)-3]
			if strings.Contains(path, "/"+dirPart+"/") {
				return true
			}
		}
	}

	// Try simple pattern match
	return matchSimple(pattern, path)
}

// matchSimple performs simple glob matching with * wildcards.
func matchSimple(pattern, str string) bool {
	// Convert glob pattern to regex
	regexPattern := "^"
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				regexPattern += ".*"
				i++ // Skip next *
			} else {
				regexPattern += "[^/]*"
			}
		case '?':
			regexPattern += "."
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			regexPattern += "\\" + string(pattern[i])
		default:
			regexPattern += string(pattern[i])
		}
	}
	regexPattern += "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}
	return re.MatchString(str)
}
