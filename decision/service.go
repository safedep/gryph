// Package decision is the boundary between the hook side, which runs as the
// agent user, and the decision service, which audits and decides. Requests
// and responses hold data only, so a later privileged supervisor can serve
// the same Service over IPC.
package decision

import (
	"context"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/security"
)

// Service handles one hook invocation end to end.
type Service interface {
	Handle(ctx context.Context, req *HookRequest) (*HookResponse, error)
	// ReportHookError records a hook invocation that produced no decision,
	// so the audit trail shows that the agent acted without one.
	ReportHookError(ctx context.Context, e *HookError) error
}

// HookError describes a hook invocation that produced no decision: an
// unknown agent, a payload that did not parse, or a failed Handle. The
// service caps and redacts RawEvent before it stores anything.
type HookError struct {
	Agent    string `json:"agent"`
	HookType string `json:"hook_type"`
	RawSize  int    `json:"raw_size"`
	RawEvent []byte `json:"raw_event,omitempty"`
	Message  string `json:"message"`
}

// HookRequest is what the hook side sends to the decision service. A service
// that runs outside the agent's process must treat every field as an
// untrusted claim. The event fields that events.Event excludes from JSON
// travel only as the explicit fields below, and NewHookRequest clears them on
// Event so each value has one source. The agent is Event.AgentName only.
type HookRequest struct {
	// HookType is the hook name the adapter recorded on the event. It can
	// differ from the CLI argument, which only selects the response format.
	HookType       events.HookType `json:"hook_type"`
	Event          events.Event    `json:"event"`
	TranscriptPath string          `json:"transcript_path,omitempty"`
	FullContent    string          `json:"full_content,omitempty"`
	// OutputTruncated is true when the hook side cut FullContent.
	OutputTruncated bool           `json:"output_truncated,omitempty"`
	Origin          privacy.Origin `json:"origin,omitempty"`
	OriginSource    string         `json:"origin_source,omitempty"`
}

// HookResponse is the decision the hook side renders for the agent.
type HookResponse struct {
	Decision Verdict `json:"decision"`
	Reason   string  `json:"reason,omitempty"`
	Guidance string  `json:"guidance,omitempty"`
}

// Verdict is the name of a decision on the wire. It keeps a name that this
// binary does not know, and its zero value holds no decision. The hook side
// blocks on both, so a newer service or a response with no decision fails
// closed.
type Verdict string

// VerdictOf returns the wire name of d.
func VerdictOf(d security.Decision) Verdict {
	return Verdict(d.String())
}

// Decision returns the decision that v names. It reports false when v is
// empty or names a decision that this binary does not know.
func (v Verdict) Decision() (security.Decision, bool) {
	return security.ParseDecision(string(v))
}

// NewHookRequest builds a request from a parsed event.
func NewHookRequest(event *events.Event) *HookRequest {
	req := &HookRequest{
		HookType:        event.HookType,
		Event:           *event,
		TranscriptPath:  event.TranscriptPath,
		FullContent:     event.FullContent,
		OutputTruncated: event.OutputTruncated,
		Origin:          event.Origin,
		OriginSource:    event.OriginSource,
	}
	req.Event.TranscriptPath = ""
	req.Event.HookType = ""
	req.Event.FullContent = ""
	return req
}

func (r *HookRequest) event() *events.Event {
	event := r.Event
	event.TranscriptPath = r.TranscriptPath
	event.HookType = r.HookType
	event.FullContent = r.FullContent
	event.OutputTruncated = r.OutputTruncated
	event.Origin = r.Origin
	event.OriginSource = r.OriginSource
	return &event
}
