package events

import (
	"time"

	"github.com/safedep/gryph/core/privacy"
)

// HookType is the name an agent gives one of its hook events, such as
// "PreToolUse" or "beforeShellExecution".
type HookType string

// Phase says whether a hook fires before the operation runs, and can still
// stop it, or after it ran.
type Phase string

const (
	PhaseUnknown Phase = "unknown"
	PhasePre     Phase = "pre"
	PhasePost    Phase = "post"
)

// HookSpec declares one hook that an adapter installs and parses.
type HookSpec struct {
	Type HookType
	// Phase is PhaseUnknown for lifecycle hooks, such as session start.
	Phase Phase
	// Blocking is true when a block response from Gryph stops the operation.
	Blocking bool
	// Prompt is true when the hook carries a user prompt.
	Prompt bool
	// MinVersion is the first agent version that fires the hook. It is empty
	// when every supported version fires it.
	MinVersion string
	// Timeout is how long the agent waits for the hook before it gives up
	// and, for every supported agent, lets the action through. It comes from
	// the agent's documented default, or from the timeout that Gryph writes
	// into the hook entry or plugin. Zero means the agent documents none.
	Timeout time.Duration
}

// WithTimeout sets Timeout on every spec that has none and returns specs.
func WithTimeout(d time.Duration, specs []HookSpec) []HookSpec {
	for i := range specs {
		if specs[i].Timeout == 0 {
			specs[i].Timeout = d
		}
	}
	return specs
}

// FailColumn names how a hook fails when Gryph cannot decide in time: it
// selects the column of the fail-mode table that an administrator sets.
type FailColumn string

const (
	// FailColumnBlocking is a pre hook whose block stops the action and that
	// carries no prompt.
	FailColumnBlocking FailColumn = "blocking"
	// FailColumnPrompt is a hook that carries the user prompt.
	FailColumnPrompt FailColumn = "prompt"
	// FailColumnOther is every other hook: post hooks and lifecycle hooks
	// whose block stops nothing.
	FailColumnOther FailColumn = "other"
)

// FailColumn returns the fail-mode column of the hook. A prompt hook uses
// the prompt column even when it blocks, because a blocked prompt stops the
// user, not the agent.
func (s HookSpec) FailColumn() FailColumn {
	switch {
	case s.Prompt:
		return FailColumnPrompt
	case s.Phase == PhasePre && s.Blocking:
		return FailColumnBlocking
	default:
		return FailColumnOther
	}
}

// Kind classifies an event in the session context.
type Kind string

const (
	// KindIntent is what the user asked for.
	KindIntent Kind = "intent"
	// KindAction is what the agent tried to do.
	KindAction Kind = "action"
	// KindObservation is what the agent received back from an action that
	// Gryph already saw at a pre hook.
	KindObservation Kind = "observation"
)

// KindOf returns the kind of an event. A prompt that a person typed is an
// intent. A prompt that agent code injected (origin agent) is an
// observation, so it never becomes the intent of the session. linked
// is true when the event is a post event whose pre event Gryph recorded. A
// post event with no linked pre event is an action, so agents with post-only
// coverage still count it.
func KindOf(e *Event, linked bool) Kind {
	switch {
	case e == nil:
		return KindAction
	case e.ActionType == ActionUserPrompt && e.Origin == privacy.OriginAgent:
		return KindObservation
	case e.ActionType == ActionUserPrompt:
		return KindIntent
	case e.Phase == PhasePost && linked:
		return KindObservation
	default:
		return KindAction
	}
}
