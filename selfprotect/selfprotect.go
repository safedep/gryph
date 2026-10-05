// Package selfprotect describes how well each Gryph asset resists a change.
// A provider assesses the assets it can see and gives each one a level. The
// profile of an install is the label that the minimum levels earn. The same
// AssetStatus feeds gryph doctor, tamper events and status reports, so the
// vocabulary lives here and nowhere else.
package selfprotect

import (
	"context"
	"fmt"
)

// Asset is a part of Gryph that an adversary can change to weaken it.
type Asset string

const (
	// AssetHookConfig is the hook configuration of one agent. The Agent
	// field of an AssetStatus names the agent.
	AssetHookConfig Asset = "hook_config"
	// AssetBinary is the gryph binary that the hook entries run.
	AssetBinary Asset = "binary"
	// AssetPolicy is the policy file and the policies directory.
	AssetPolicy Asset = "policy"
	// AssetConfig is the configuration file in force.
	AssetConfig Asset = "config"
	// AssetStore is the audit database.
	AssetStore Asset = "store"
	// AssetKey is a secret key: the receipt signing key or the export key.
	AssetKey Asset = "key"
	// AssetSupervisor is the decision service process. No provider assesses
	// it until a system service exists.
	AssetSupervisor Asset = "supervisor"
	// AssetHookTraffic is the flow of hook calls from a live agent process.
	// The Agent field names the agent. A live agent with no hook calls is
	// drift: the hooks do not reach Gryph.
	AssetHookTraffic Asset = "hook_traffic"
)

// Level is how far a provider protects an asset. The order matters: a
// profile demands a minimum level per asset.
type Level int

const (
	// LevelNone means nothing stops or notices a change.
	LevelNone Level = iota
	// LevelMediated means the built-in policy rules block a change, but only
	// through a hooked tool call.
	LevelMediated
	// LevelDetect means Gryph notices a change and records it.
	LevelDetect
	// LevelRepair means Gryph restores the asset and records the repair.
	LevelRepair
	// LevelPreventSameUser means the operating system stops a change by a
	// non-admin process of the agent's user.
	LevelPreventSameUser
	// LevelPreventRootBestEffort means a kernel mechanism raises the cost of
	// a change for root. It never stops root.
	LevelPreventRootBestEffort
)

var levelNames = map[Level]string{
	LevelNone:                  "none",
	LevelMediated:              "mediated",
	LevelDetect:                "detect",
	LevelRepair:                "repair",
	LevelPreventSameUser:       "prevent_same_user",
	LevelPreventRootBestEffort: "prevent_root_best_effort",
}

// String returns the wire name of the level.
func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "none"
}

// AssetStatus is the assessment of one asset by one provider.
type AssetStatus struct {
	Asset Asset
	// Agent names the agent of a hook configuration. It is empty for every
	// other asset.
	Agent string
	Level Level
	// Attest is true when evidence of this asset's state exists off the host.
	Attest   bool
	Provider string
	// Drift says how the asset differs from its desired state. It is empty
	// when the asset matches.
	Drift string
	// Detail names the asset on this host, for example its path.
	Detail string
}

// RepairOptions configures one repair pass.
type RepairOptions struct {
	// DryRun reports the repairs without making them.
	DryRun bool
	// Only limits the repair to these assets. Empty repairs every asset
	// with drift that the provider owns.
	Only []AssetRef
}

// AssetRef names one asset: the asset kind and, for a hook configuration,
// the agent.
type AssetRef struct {
	Asset Asset
	Agent string
}

// Ref returns the reference of the status.
func (s AssetStatus) Ref() AssetRef { return AssetRef{Asset: s.Asset, Agent: s.Agent} }

// RepairError is the error of one asset that a repair did not restore. A
// Repair call returns it joined with the others, so the caller can name
// each asset.
type RepairError struct {
	Ref AssetRef
	Err error
}

func (e *RepairError) Error() string {
	if e.Ref.Agent != "" {
		return fmt.Sprintf("%s %s: %v", e.Ref.Asset, e.Ref.Agent, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Ref.Asset, e.Err)
}

func (e *RepairError) Unwrap() error { return e.Err }

// Provider assesses and repairs the assets it owns.
type Provider interface {
	Name() string
	// Assess is read only and cheap. gryph doctor, a reconcile pass and a
	// status report run it.
	Assess(ctx context.Context) []AssetStatus
	// Repair restores the drift that this provider owns and returns the
	// assets it changed.
	Repair(ctx context.Context, opts RepairOptions) ([]AssetStatus, error)
}

// Profile is the label that the minimum levels of an install earn.
type Profile string

const (
	// ProfileNone means the install does not reach Guard.
	ProfileNone Profile = "none"
	// ProfileGuard means the built-in rules cover every asset and Gryph
	// detects a change to a hook configuration.
	ProfileGuard Profile = "guard"
	// ProfileLocked means the operating system stops a non-admin user from
	// changing the binary, the policy and the config, and Gryph repairs a
	// hook configuration.
	ProfileLocked Profile = "locked"
	// ProfileManaged means Locked with off-host evidence for every asset.
	ProfileManaged Profile = "managed"
)

// ProfileOf returns the profile that statuses earn. It takes the minimum
// level per asset, so one weak agent lowers the profile of the host.
func ProfileOf(statuses []AssetStatus) Profile {
	if len(statuses) == 0 {
		return ProfileNone
	}
	if !every(statuses, func(s AssetStatus) bool { return s.Level >= LevelMediated }) ||
		!everyOf(statuses, AssetHookConfig, LevelDetect) {
		return ProfileNone
	}
	if !everyOf(statuses, AssetBinary, LevelPreventSameUser) ||
		!everyOf(statuses, AssetPolicy, LevelPreventSameUser) ||
		!everyOf(statuses, AssetConfig, LevelPreventSameUser) ||
		!everyOf(statuses, AssetHookConfig, LevelRepair) {
		return ProfileGuard
	}
	if !every(statuses, func(s AssetStatus) bool { return s.Attest }) {
		return ProfileLocked
	}
	return ProfileManaged
}

func every(statuses []AssetStatus, ok func(AssetStatus) bool) bool {
	for _, s := range statuses {
		if !ok(s) {
			return false
		}
	}
	return true
}

func everyOf(statuses []AssetStatus, asset Asset, min Level) bool {
	return every(statuses, func(s AssetStatus) bool { return s.Asset != asset || s.Level >= min })
}
