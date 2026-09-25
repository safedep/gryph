package events

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
}

// Kind classifies an event in the session context.
type Kind string

const (
	// KindAction is what the agent tried to do.
	KindAction Kind = "action"
	// KindObservation is what the agent received back from an action that
	// Gryph already saw at a pre hook.
	KindObservation Kind = "observation"
)

// KindOf returns the kind of an event. linked is true when the event is a
// post event whose pre event Gryph recorded. A post event with no linked pre
// event is an action, so agents with post-only coverage still count it.
func KindOf(e *Event, linked bool) Kind {
	if e != nil && e.Phase == PhasePost && linked {
		return KindObservation
	}
	return KindAction
}
