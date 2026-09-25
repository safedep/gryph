// Package decision is the boundary between the hook side, which runs as the
// agent user, and the decision service, which audits and decides. Requests
// and responses hold data only, so a later privileged supervisor can serve
// the same Service over IPC.
//
// Known gap: the hook command still opens the store to write the hook-error
// self-audit row. The supervisor work moves that write behind the service.
package decision

import (
	"context"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/security"
)

// Service handles one hook invocation end to end.
type Service interface {
	Handle(ctx context.Context, req *HookRequest) (*HookResponse, error)
}

// HookRequest is what the hook side sends to the decision service. A service
// that runs outside the agent's process must treat every field as an
// untrusted claim. The event fields that events.Event excludes from JSON
// travel only as the explicit fields below, and NewHookRequest clears them on
// Event so each value has one source.
type HookRequest struct {
	Agent string `json:"agent"`
	// HookType is the hook name the adapter recorded on the event. It can
	// differ from the CLI argument, which only selects the response format.
	HookType       string       `json:"hook_type"`
	Event          events.Event `json:"event"`
	TranscriptPath string       `json:"transcript_path,omitempty"`
	FullContent    string       `json:"full_content,omitempty"`
}

// HookResponse is the decision the hook side renders for the agent.
type HookResponse struct {
	Decision security.Decision `json:"decision"`
	Reason   string            `json:"reason,omitempty"`
	Guidance string            `json:"guidance,omitempty"`
}

// NewHookRequest builds a request from a parsed event.
func NewHookRequest(agent string, event *events.Event) *HookRequest {
	req := &HookRequest{
		Agent:          agent,
		HookType:       event.HookType,
		Event:          *event,
		TranscriptPath: event.TranscriptPath,
		FullContent:    event.FullContent,
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
	return &event
}
