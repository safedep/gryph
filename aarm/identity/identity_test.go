package identity

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ciEnvVars is the union of env-vars the CI auto-detection inspects. Tests
// clear them via t.Setenv("", "") so a host build environment cannot leak
// into the unit test.
var ciEnvVars = []string{
	"GITHUB_ACTIONS", "GITHUB_REPOSITORY", "GITHUB_WORKFLOW", "GITHUB_RUN_ID",
	"BUILDKITE", "BUILDKITE_PIPELINE_SLUG", "BUILDKITE_BUILD_NUMBER",
	"GITLAB_CI", "CI_PROJECT_PATH", "CI_PIPELINE_ID",
	"CIRCLECI", "CIRCLE_PROJECT_REPONAME", "CIRCLE_BUILD_NUM",
	"CI",
}

func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvHumanPrincipal, "")
	t.Setenv(EnvServiceIdentity, "")
	t.Setenv(EnvRoleScope, "")
	for _, name := range ciEnvVars {
		t.Setenv(name, "")
	}
}

func TestResolveHumanPrincipal_EnvOverrideWins(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvHumanPrincipal, "alice@example.com")

	got := resolve()
	assert.Equal(t, "alice@example.com", got.HumanPrincipal)
}

func TestResolveHumanPrincipal_OSLookupFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")

	got := resolve()
	require.NotEmpty(t, got.HumanPrincipal,
		"expected uid:N:username form when env unset and OS user lookup works")
	assert.True(t, strings.HasPrefix(got.HumanPrincipal, "uid:"),
		"want uid: prefix, got %q", got.HumanPrincipal)

	uid := os.Getuid()
	u, lookupErr := user.LookupId(fmt.Sprintf("%d", uid))
	if lookupErr == nil && u != nil && u.Username != "" {
		assert.Equal(t, fmt.Sprintf("uid:%d:%s", uid, u.Username), got.HumanPrincipal)
	}
}

func TestResolveHumanPrincipal_UserEnvFallback(t *testing.T) {
	clearEnv(t)
	uid := os.Getuid()
	u, lookupErr := user.LookupId(fmt.Sprintf("%d", uid))
	if lookupErr == nil && u != nil && u.Username != "" {
		t.Skip("OS user lookup works, USER fallback path is not reachable on this host")
	}
	t.Setenv("USER", "fallback-user")
	got := resolve()
	assert.Equal(t, fmt.Sprintf("uid:%d:fallback-user", uid), got.HumanPrincipal)
}

func TestResolveHumanPrincipal_NegativeUIDDropsUIDPortion(t *testing.T) {
	clearEnv(t)
	t.Setenv("USER", "fallback-user")
	t.Setenv("LOGNAME", "")

	origUID := getuidFn
	getuidFn = func() int { return -1 }
	t.Cleanup(func() { getuidFn = origUID })

	got := resolve()
	assert.Equal(t, "user:fallback-user", got.HumanPrincipal,
		"on platforms where uid is unavailable (Windows: uid=-1), the principal must not embed -1")
}

func TestResolveRoleScope_NegativeIDsDropsUIDPortion(t *testing.T) {
	clearEnv(t)

	origUID, origEUID, origGID := getuidFn, geteuidFn, getgidFn
	getuidFn = func() int { return -1 }
	geteuidFn = func() int { return -1 }
	getgidFn = func() int { return -1 }
	t.Cleanup(func() {
		getuidFn = origUID
		geteuidFn = origEUID
		getgidFn = origGID
	})

	got := resolve()
	assert.NotContains(t, got.RoleScope, "-1",
		"role scope must not embed -1 when OS uid/euid/gid are unavailable")
	if got.RoleScope != "" {
		assert.True(t, strings.HasPrefix(got.RoleScope, "user="),
			"when uids are unavailable role scope falls back to user=<name>, got %q", got.RoleScope)
	}
}

func TestResolveServiceIdentity_EnvOverrideWins(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvServiceIdentity, "operator-asserted-value")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "ignored/repo")

	got := resolve()
	assert.Equal(t, "operator-asserted-value", got.ServiceIdentity)
}

func TestResolveServiceIdentity_GitHubActions(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "safedep/gryph")
	t.Setenv("GITHUB_WORKFLOW", "release")
	t.Setenv("GITHUB_RUN_ID", "123")

	got := resolve()
	assert.Equal(t, "github-actions:safedep/gryph#release@123", got.ServiceIdentity)
}

func TestResolveServiceIdentity_GitHubActions_NoRunID(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "safedep/gryph")
	t.Setenv("GITHUB_WORKFLOW", "release")

	got := resolve()
	assert.Equal(t, "github-actions:safedep/gryph#release", got.ServiceIdentity)
}

func TestResolveServiceIdentity_Buildkite(t *testing.T) {
	clearEnv(t)
	t.Setenv("BUILDKITE", "true")
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "pipe")
	t.Setenv("BUILDKITE_BUILD_NUMBER", "42")

	got := resolve()
	assert.Equal(t, "buildkite:pipe#42", got.ServiceIdentity)
}

func TestResolveServiceIdentity_GitLab(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PROJECT_PATH", "grp/proj")
	t.Setenv("CI_PIPELINE_ID", "99")

	got := resolve()
	assert.Equal(t, "gitlab-ci:grp/proj#99", got.ServiceIdentity)
}

func TestResolveServiceIdentity_CircleCI(t *testing.T) {
	clearEnv(t)
	t.Setenv("CIRCLECI", "true")
	t.Setenv("CIRCLE_PROJECT_REPONAME", "gryph")
	t.Setenv("CIRCLE_BUILD_NUM", "7")

	got := resolve()
	assert.Equal(t, "circleci:gryph#7", got.ServiceIdentity)
}

func TestResolveServiceIdentity_GenericCI(t *testing.T) {
	clearEnv(t)
	t.Setenv("CI", "true")

	got := resolve()
	assert.Equal(t, "ci:unknown", got.ServiceIdentity)
}

func TestResolveServiceIdentity_EmptyWhenNoCI(t *testing.T) {
	clearEnv(t)
	got := resolve()
	assert.Empty(t, got.ServiceIdentity)
}

func TestResolveServiceIdentity_GitHubActionsBeforeGenericCI(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "safedep/gryph")
	t.Setenv("GITHUB_WORKFLOW", "ci")
	t.Setenv("CI", "true")

	got := resolve()
	assert.True(t, strings.HasPrefix(got.ServiceIdentity, "github-actions:"),
		"specific matcher must win over generic CI fallback, got %q", got.ServiceIdentity)
}

func TestResolveRoleScope_EnvOverrideWins(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvRoleScope, "role:admin,group:eng")

	got := resolve()
	assert.Equal(t, "role:admin,group:eng", got.RoleScope)
}

func TestResolveRoleScope_OSDerived(t *testing.T) {
	clearEnv(t)
	got := resolve()
	require.NotEmpty(t, got.RoleScope, "OS uid/gid is always available on the test host")
	assert.Contains(t, got.RoleScope, fmt.Sprintf("uid=%d", os.Getuid()))
	assert.Contains(t, got.RoleScope, fmt.Sprintf("euid=%d", os.Geteuid()))
}

func TestNewDefaultCapturer_CachesResult(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvHumanPrincipal, "first@example.com")

	c := NewDefaultCapturer()
	first := c.Capture(context.Background())
	assert.Equal(t, "first@example.com", first.HumanPrincipal)

	t.Setenv(EnvHumanPrincipal, "mutated@example.com")
	second := c.Capture(context.Background())
	assert.Equal(t, "first@example.com", second.HumanPrincipal,
		"capturer must cache the construction-time resolution")
}

func TestStaticCapturer(t *testing.T) {
	want := Capture{
		HumanPrincipal:  "alice",
		ServiceIdentity: "ci:test",
		RoleScope:       "role:admin",
	}
	c := NewStaticCapturer(want)
	got := c.Capture(context.Background())
	assert.Equal(t, want, got)
}
