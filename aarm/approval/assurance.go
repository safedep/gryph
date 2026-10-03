package approval

import (
	"fmt"
	"slices"
	"strings"
)

// Assurance names how sure Gryph is about who answered an approval. Each
// channel gives one assurance. The order below is the rank order: a rule's
// min_assurance names the lowest rank it accepts, and a channel below it
// never answers that rule.
type Assurance string

const (
	// AssuranceSameUserTTY is the prompt on the developer's own terminal.
	// The same account can forge it, so it applies to one request only and
	// stores no grant.
	AssuranceSameUserTTY Assurance = "same-user-tty"
	// AssuranceSelfElevated is a local admin who is the same person as the
	// developer, through sudo. Off by default.
	AssuranceSelfElevated Assurance = "self-elevated"
	// AssuranceLocalAdmin is a member of the admin group on the host, as
	// the peer credential of the socket reports it.
	AssuranceLocalAdmin Assurance = "local-admin"
	// AssuranceLocalAuth is an answer behind an authentication prompt of
	// the operating system.
	AssuranceLocalAuth Assurance = "local-auth"
	// AssuranceOutOfBand is an approver outside the host, with an identity
	// of their own.
	AssuranceOutOfBand Assurance = "out-of-band"
)

// Assurances lists every assurance in rank order, lowest first.
var Assurances = []Assurance{AssuranceSameUserTTY, AssuranceSelfElevated, AssuranceLocalAdmin, AssuranceLocalAuth, AssuranceOutOfBand}

// Rank returns the position of a in the order, from 1. An unknown
// assurance ranks 0, below every known one.
func (a Assurance) Rank() int {
	return slices.Index(Assurances, a) + 1
}

// Meets reports whether a is at least min. An empty min is no floor.
func (a Assurance) Meets(min Assurance) bool {
	if min == "" {
		return a.Rank() > 0
	}
	return a.Rank() >= min.Rank()
}

// ParseAssurance returns the assurance that s names.
func ParseAssurance(s string) (Assurance, error) {
	a := Assurance(strings.TrimSpace(s))
	if a.Rank() == 0 {
		return "", fmt.Errorf("unknown assurance %q: must be one of %s", s, strings.Join(assuranceNames(), ", "))
	}
	return a, nil
}

func assuranceNames() []string {
	out := make([]string, len(Assurances))
	for i, a := range Assurances {
		out[i] = string(a)
	}
	return out
}

// Channel is an approval Service that knows the assurance of its answers.
// The Mediator refuses a request whose min_assurance the service cannot
// meet, so a rule never falls back to a weaker channel.
type Channel interface {
	Assurance() Assurance
}

// Scope says what a stored grant applies to.
type Scope string

const (
	// ScopeOnce matches the same action digest one time.
	ScopeOnce Scope = "once"
	// ScopeSession matches the same action digest for the rest of the
	// agent session.
	ScopeSession Scope = "session"
	// ScopeWindow matches the same action digest for the account until the
	// grant expires.
	ScopeWindow Scope = "window"
)

// Scopes lists every scope, narrowest first.
var Scopes = []Scope{ScopeOnce, ScopeSession, ScopeWindow}

// ParseScope returns the scope that s names.
func ParseScope(s string) (Scope, error) {
	sc := Scope(strings.TrimSpace(s))
	if !slices.Contains(Scopes, sc) {
		return "", fmt.Errorf("unknown scope %q: must be once, session or window", s)
	}
	return sc, nil
}

// Wider reports whether a is wider than b.
func (a Scope) Wider(b Scope) bool {
	return slices.Index(Scopes, a) > slices.Index(Scopes, b)
}

// Peer trust levels of the connection that carried an approval. The
// decision service records one on every approval receipt.
const (
	// PeerTrustUnknown says the service did not check the process behind
	// the connection.
	PeerTrustUnknown = "unknown"
)
