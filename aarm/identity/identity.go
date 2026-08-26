// Package identity captures the human principal, service identity, and
// role/privilege scope at the AARM mediation boundary. The captured values
// populate model.Action fields and surface on the receipt row so downstream
// auditors can attribute every mediated action.
//
// Capture is read-only: it inspects environment variables, OS process
// credentials, and well-known CI signals once at construction. The
// DefaultCapturer caches the result for the process lifetime because identity
// does not change while Gryph runs. Tests inject custom Capturer
// implementations.
package identity

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"
	"syscall"

	"github.com/safedep/gryph/aarm/approval"
)

// Capture is the immutable result of one identity-resolution pass.
type Capture struct {
	HumanPrincipal  string
	ServiceIdentity string
	RoleScope       string
}

// Capturer returns the captured identity. Implementations must be safe for
// concurrent calls.
type Capturer interface {
	Capture(ctx context.Context) Capture
}

// Env names for operator-asserted overrides.
const (
	EnvHumanPrincipal  = "GRYPH_HUMAN_PRINCIPAL"
	EnvServiceIdentity = "GRYPH_SERVICE_IDENTITY"
	EnvRoleScope       = "GRYPH_ROLE_SCOPE"
)

const roleScopeMaxGroups = 8

// getuidFn / geteuidFn / getgidFn are package-level indirections over the
// matching os.* functions so tests can simulate platforms where the UID
// lookup is unsupported (notably Windows, where os.Getuid returns -1).
var (
	getuidFn  = os.Getuid
	geteuidFn = os.Geteuid
	getgidFn  = os.Getgid
)

// formatPrincipal renders a captured username into the canonical principal
// string. When uid is non-negative the form is "uid:<n>:<name>". When uid
// is negative (e.g. Windows, where os.Getuid returns -1) the uid portion is
// dropped to avoid emitting confusing values like "uid:-1:<name>".
func formatPrincipal(uid int, name string) string {
	if uid >= 0 {
		return fmt.Sprintf("uid:%d:%s", uid, name)
	}
	return fmt.Sprintf("user:%s", name)
}

// DefaultCapturer reads env + OS once at construction and returns the cached
// result from every Capture call.
type DefaultCapturer struct {
	cached Capture
}

// NewDefaultCapturer resolves the three identity fields per spec section 2 and
// caches them. Subsequent Capture calls return the cached values without
// re-reading the environment.
func NewDefaultCapturer() *DefaultCapturer {
	return &DefaultCapturer{cached: resolve()}
}

// Capture implements Capturer.
func (d *DefaultCapturer) Capture(_ context.Context) Capture {
	if d == nil {
		return Capture{}
	}
	return d.cached
}

// StaticCapturer returns a fixed Capture. Used by production code to disable
// identity capture and by tests to inject a deterministic value.
type StaticCapturer struct {
	Value Capture
}

// NewStaticCapturer returns a Capturer that always returns v.
func NewStaticCapturer(v Capture) *StaticCapturer {
	return &StaticCapturer{Value: v}
}

// Capture implements Capturer.
func (s *StaticCapturer) Capture(_ context.Context) Capture {
	if s == nil {
		return Capture{}
	}
	return s.Value
}

func resolve() Capture {
	return Capture{
		HumanPrincipal:  resolveHumanPrincipal(),
		ServiceIdentity: resolveServiceIdentity(),
		RoleScope:       resolveRoleScope(),
	}
}

func resolveHumanPrincipal() string {
	if v := strings.TrimSpace(os.Getenv(EnvHumanPrincipal)); v != "" {
		return v
	}
	uid := getuidFn()
	if uid >= 0 {
		if u, err := user.LookupId(fmt.Sprintf("%d", uid)); err == nil && u != nil && u.Username != "" {
			return formatPrincipal(uid, u.Username)
		}
	}
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return formatPrincipal(uid, name)
	}
	if name := strings.TrimSpace(os.Getenv("LOGNAME")); name != "" {
		return formatPrincipal(uid, name)
	}
	return ""
}

func resolveServiceIdentity() string {
	if v := strings.TrimSpace(os.Getenv(EnvServiceIdentity)); v != "" {
		return v
	}
	if v := detectCIService(); v != "" {
		return v
	}
	return ""
}

func detectCIService() string {
	if strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true") {
		repo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
		workflow := strings.TrimSpace(os.Getenv("GITHUB_WORKFLOW"))
		runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
		base := fmt.Sprintf("github-actions:%s#%s", repo, workflow)
		if runID != "" {
			return base + "@" + runID
		}
		return base
	}
	if strings.EqualFold(os.Getenv("BUILDKITE"), "true") {
		pipeline := strings.TrimSpace(os.Getenv("BUILDKITE_PIPELINE_SLUG"))
		build := strings.TrimSpace(os.Getenv("BUILDKITE_BUILD_NUMBER"))
		return fmt.Sprintf("buildkite:%s#%s", pipeline, build)
	}
	if strings.EqualFold(os.Getenv("GITLAB_CI"), "true") {
		project := strings.TrimSpace(os.Getenv("CI_PROJECT_PATH"))
		pipeline := strings.TrimSpace(os.Getenv("CI_PIPELINE_ID"))
		return fmt.Sprintf("gitlab-ci:%s#%s", project, pipeline)
	}
	if strings.EqualFold(os.Getenv("CIRCLECI"), "true") {
		repo := strings.TrimSpace(os.Getenv("CIRCLE_PROJECT_REPONAME"))
		build := strings.TrimSpace(os.Getenv("CIRCLE_BUILD_NUM"))
		return fmt.Sprintf("circleci:%s#%s", repo, build)
	}
	if strings.EqualFold(os.Getenv("CI"), "true") {
		return "ci:unknown"
	}
	return ""
}

func resolveRoleScope() string {
	if v := strings.TrimSpace(os.Getenv(EnvRoleScope)); v != "" {
		return v
	}
	return osRoleScope()
}

func osRoleScope() string {
	uid := getuidFn()
	euid := geteuidFn()
	gid := getgidFn()
	if uid < 0 && euid < 0 && gid < 0 {
		if name := osUsernameFallback(); name != "" {
			return "user=" + name
		}
		return ""
	}
	parts := make([]string, 0, 4)
	if uid >= 0 {
		parts = append(parts, fmt.Sprintf("uid=%d", uid))
	}
	if euid >= 0 {
		parts = append(parts, fmt.Sprintf("euid=%d", euid))
	}
	if gid >= 0 {
		parts = append(parts, fmt.Sprintf("gid=%d", gid))
	}
	if groups, err := syscall.Getgroups(); err == nil && len(groups) > 0 {
		sort.Ints(groups)
		if len(groups) > roleScopeMaxGroups {
			groups = groups[:roleScopeMaxGroups]
		}
		strs := make([]string, len(groups))
		for i, g := range groups {
			strs[i] = fmt.Sprintf("%d", g)
		}
		parts = append(parts, "groups="+strings.Join(strs, ":"))
	}
	return strings.Join(parts, ",")
}

// osUsernameFallback returns the current OS username without relying on a
// numeric UID. Used on platforms where os.Getuid returns -1 (Windows). Falls
// back to the shared approval.OSUsernameOrDefault env walk so the Windows
// USERNAME variable is honored from this call site too.
func osUsernameFallback() string {
	if u, err := user.Current(); err == nil && u != nil && u.Username != "" {
		return u.Username
	}
	return approval.OSUsernameOrDefault("")
}
