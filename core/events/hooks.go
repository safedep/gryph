package events

import "github.com/safedep/gryph/core/privacy"

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
