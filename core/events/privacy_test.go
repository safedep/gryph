package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrivacyChecker(t *testing.T) {
	checker, err := NewPrivacyChecker(
		[]string{"**/.env"},
		[]string{`password=\S+`},
	)
	require.NoError(t, err)
	require.NotNil(t, checker)
}

func TestNewPrivacyChecker_InvalidRegex(t *testing.T) {
	checker, err := NewPrivacyChecker(
		[]string{"**/.env"},
		[]string{`[invalid regex`},
	)
	assert.Error(t, err)
	assert.Nil(t, checker)
}

func TestDefaultSensitivePatterns(t *testing.T) {
	patterns := DefaultSensitivePatterns()
	assert.NotEmpty(t, patterns)
	assert.Contains(t, patterns, "**/.env")
	assert.Contains(t, patterns, "**/*.pem")
	assert.Contains(t, patterns, "**/.ssh/**")
	assert.Contains(t, patterns, "**/.aws/**")
}

func TestDefaultRedactPatterns(t *testing.T) {
	patterns := DefaultRedactPatterns()
	assert.NotEmpty(t, patterns)
}

func TestPrivacyChecker_IsSensitivePath_EnvFiles(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"/home/user/project/.env", true},
		{"/home/user/project/.env.local", true},
		{"/home/user/project/.env.production", true},
		{".env", true},
		{"/project/.env", true},
		{"/home/user/project/config.yaml", false},
		{"/home/user/project/main.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_IsSensitivePath_KeyFiles(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"/home/user/.ssh/id_rsa", true},
		{"/home/user/.ssh/id_rsa.pub", true},
		{"/home/user/.ssh/config", true},
		{"/home/user/.ssh/known_hosts", true},
		{"/home/user/project/server.pem", true},
		{"/home/user/project/server.key", true},
		{"/home/user/project/certificate.p12", true},
		{"/home/user/project/readme.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_IsSensitivePath_CloudCredentials(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"/home/user/.aws/credentials", true},
		{"/home/user/.aws/config", true},
		{"/home/user/.npmrc", true},
		{"/home/user/.pypirc", true},
		{"/home/user/.git/config", true},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_IsSensitivePath_SecretsDirectory(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"/home/user/project/secrets/api-key.txt", true},
		{"/home/user/secrets/config.json", true},
		{"/secrets/data.yaml", true},
		// Note: secrets.go DOES match **/*secret* pattern (filename contains "secret")
		{"/home/user/project/src/secrets.go", true},
		// But a file without secret in the name in a non-secrets directory is not sensitive
		{"/home/user/project/src/config.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_IsSensitivePath_PasswordAndSecretInName(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"/home/user/project/password.txt", true},
		{"/home/user/project/passwords.json", true},
		{"/home/user/project/secret.yaml", true},
		{"/home/user/project/secrets.env", true},
		{"/home/user/project/credential.json", true},
		{"/home/user/project/credentials.yaml", true},
		{"/home/user/project/db_password", true},
		{"/home/user/project/api_secret", true},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_IsSensitivePath_ForwardSlashPaths(t *testing.T) {
	checker, err := NewPrivacyChecker(DefaultSensitivePatterns(), nil)
	require.NoError(t, err)

	// Test forward-slash paths (cross-platform compatible)
	// Note: Windows backslash paths only work correctly on Windows
	// because filepath.ToSlash behaves differently on each platform
	testCases := []struct {
		path      string
		sensitive bool
	}{
		{"C:/Users/dev/project/.env", true},
		{"C:/Users/dev/.ssh/id_rsa", true},
		{"C:/Users/dev/project/main.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := checker.IsSensitivePath(tc.path)
			assert.Equal(t, tc.sensitive, result, "Path: %s", tc.path)
		})
	}
}

func TestPrivacyChecker_Redact_Passwords(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"password=secret123", "[REDACTED]"},
		{"PASSWORD=secret123", "[REDACTED]"},
		{"password:mysecret", "[REDACTED]"},
		{"export PASSWORD=test123", "export [REDACTED]"},
		{"no password here", "no password here"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := checker.Redact(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPrivacyChecker_Redact_APIKeys(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"api_key=abc123xyz", "[REDACTED]"},
		{"API_KEY=abc123xyz", "[REDACTED]"},
		{"api-key=abc123xyz", "[REDACTED]"},
		{"apikey=abc123xyz", "[REDACTED]"},
		{"APIKEY:secret_value", "[REDACTED]"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := checker.Redact(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPrivacyChecker_Redact_Tokens(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"token=abc123", "[REDACTED]"},
		{"TOKEN=abc123", "[REDACTED]"},
		{"token:xyz789", "[REDACTED]"},
		{"access_token=mytoken", "access_[REDACTED]"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := checker.Redact(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPrivacyChecker_Redact_BearerTokens(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "Authorization: [REDACTED]"},
		{"bearer abc123", "[REDACTED]"},
		{"BEARER xyz789", "[REDACTED]"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := checker.Redact(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPrivacyChecker_Redact_AWSCredentials(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"aws_access_key_id=AKIAIOSFODNN7EXAMPLE", "[REDACTED]"},
		{"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "[REDACTED]"},
		{"aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "[REDACTED]"},
		{"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "[REDACTED]"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := checker.Redact(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPrivacyChecker_Redact_MultipleMatches(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	input := "password=secret1 api_key=abc123 token=xyz789"
	result := checker.Redact(input)

	assert.Equal(t, "[REDACTED] [REDACTED] [REDACTED]", result)
}

func TestPrivacyChecker_Redact_NoMatches(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	input := "This is a normal log message with no sensitive data"
	result := checker.Redact(input)

	assert.Equal(t, input, result)
}

func TestPrivacyChecker_Redact_EmptyContent(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, DefaultRedactPatterns())
	require.NoError(t, err)

	result := checker.Redact("")
	assert.Equal(t, "", result)
}

func TestPrivacyChecker_CustomPatterns(t *testing.T) {
	checker, err := NewPrivacyChecker(
		[]string{"**/custom/**", "**/*.secret"},
		[]string{`custom_key=\S+`},
	)
	require.NoError(t, err)

	// Test custom sensitive paths
	assert.True(t, checker.IsSensitivePath("/home/user/custom/config.yaml"))
	assert.True(t, checker.IsSensitivePath("/home/user/project/data.secret"))
	assert.False(t, checker.IsSensitivePath("/home/user/project/.env"))

	// Test custom redaction
	assert.Equal(t, "[REDACTED]", checker.Redact("custom_key=my_value"))
	assert.Equal(t, "password=test", checker.Redact("password=test")) // Default pattern not included
}

func TestPrivacyChecker_EmptyPatterns(t *testing.T) {
	checker, err := NewPrivacyChecker(nil, nil)
	require.NoError(t, err)

	// No patterns means nothing is sensitive
	assert.False(t, checker.IsSensitivePath("/home/user/.env"))
	assert.False(t, checker.IsSensitivePath("/home/user/.ssh/id_rsa"))

	// No redaction patterns means no redaction
	assert.Equal(t, "password=secret", checker.Redact("password=secret"))
}

func TestMatchGlob_DoubleStarPrefix(t *testing.T) {
	testCases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{"**/.env", "/home/user/.env", true},
		{"**/.env", ".env", true},
		{"**/.env", "/project/subdir/.env", true},
		{"**/.env", "/home/user/.environment", false},
		{"**/*.pem", "/home/user/cert.pem", true},
		{"**/*.pem", "/home/user/subdir/server.pem", true},
		{"**/*.pem", "/home/user/cert.key", false},
	}

	for _, tc := range testCases {
		t.Run(tc.pattern+"_"+tc.path, func(t *testing.T) {
			result := matchGlob(tc.pattern, tc.path)
			assert.Equal(t, tc.matches, result, "Pattern: %s, Path: %s", tc.pattern, tc.path)
		})
	}
}

func TestMatchGlob_DirectoryPatterns(t *testing.T) {
	testCases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{"**/secrets/**", "/home/user/secrets/api.key", true},
		{"**/secrets/**", "/project/secrets/subdir/file.txt", true},
		{"**/secrets/**", "/home/user/project/src/secrets.go", false},
		{"**/.ssh/**", "/home/user/.ssh/id_rsa", true},
		{"**/.ssh/**", "/home/user/.ssh/config", true},
		{"**/.aws/**", "/home/user/.aws/credentials", true},
	}

	for _, tc := range testCases {
		t.Run(tc.pattern+"_"+tc.path, func(t *testing.T) {
			result := matchGlob(tc.pattern, tc.path)
			assert.Equal(t, tc.matches, result, "Pattern: %s, Path: %s", tc.pattern, tc.path)
		})
	}
}

func TestMatchSimple_Wildcards(t *testing.T) {
	testCases := []struct {
		pattern string
		str     string
		matches bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "test_main.go", true},
		{"*.go", "main.txt", false},
		{"test_*", "test_main", true},
		{"test_*", "main_test", false},
		{"*_test.go", "main_test.go", true},
		{"file.?", "file.a", true},
		{"file.?", "file.ab", false},
	}

	for _, tc := range testCases {
		t.Run(tc.pattern+"_"+tc.str, func(t *testing.T) {
			result := matchSimple(tc.pattern, tc.str)
			assert.Equal(t, tc.matches, result, "Pattern: %s, Str: %s", tc.pattern, tc.str)
		})
	}
}
