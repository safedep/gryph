package ipc

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"time"
	"unicode/utf8"

	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/storage"
)

// The bounds of the string fields. A field above its bound makes the frame
// invalid. The bounds are generous for a real agent and tight for a flood.
const (
	MaxName       = 64      // an agent name, a hook type, a mode, a code, a cap
	MaxVersion    = 64      // a client or server version
	MaxCaps       = 32      // capabilities in a Hello
	MaxProject    = 256     // a project claim
	MaxRawPayload = 6 << 20 // a raw agent payload
	MaxText       = 4096    // a reason, a guidance, a note, a message
	MaxDigest     = 128     // an action digest
	MaxQueryParam = 1024    // one query parameter
	MaxQueryItems = 64      // query parameters, or rows in one result
	MaxRawEvent   = 64 << 10
)

// Hello opens a connection. The client names its protocol version, its
// binary version and the capabilities it understands.
type Hello struct {
	Proto         int      `json:"proto"`
	ClientVersion string   `json:"client_version"`
	Caps          []string `json:"caps,omitempty"`
}

// Welcome answers Hello. Mode is the profile the server enforces: enforce
// or pilot. A client that gets no Welcome within its budget treats the
// server as down.
type Welcome struct {
	Proto         int    `json:"proto"`
	ServerVersion string `json:"server_version"`
	Mode          string `json:"mode"`
}

// Handle asks for a decision on one hook invocation. RawPayload is the
// agent's payload as the hook read it from stdin. The server parses it with
// its own adapter registry, so no parsed event travels on the wire and the
// client cannot forge one. The project claim and the cost totals are what
// the hook side found; the server records them as claims.
type Handle struct {
	Agent      string                `json:"agent"`
	HookType   string                `json:"hook_type"`
	RawPayload []byte                `json:"raw_payload"`
	Project    decision.ProjectClaim `json:"project_claim"`
	Cost       *cost.SessionCost     `json:"cost_totals,omitempty"`
}

// Decision answers Handle. It is the response the hook side renders for
// the agent, with the fields of decision.HookResponse. A verdict this
// binary does not know blocks on the client.
type Decision decision.HookResponse

// Response returns the decision as the hook side renders it.
func (d *Decision) Response() *decision.HookResponse {
	return (*decision.HookResponse)(d)
}

// Prompt asks the client to put a question to the human: an escalation
// that needs an answer before the deadline. The nonce ties the reply to
// the question.
type Prompt struct {
	Nonce        string    `json:"nonce"`
	ActionDigest string    `json:"action_digest"`
	Deadline     time.Time `json:"deadline"`
}

// PromptReply carries the human's answer.
type PromptReply struct {
	Nonce    string           `json:"nonce"`
	Decision decision.Verdict `json:"decision"`
	Note     string           `json:"note,omitempty"`
}

// ReportHookError tells the server that a hook invocation produced no
// decision, with the fields of decision.HookError. The server answers with
// Ack.
type ReportHookError decision.HookError

// HookError returns the report as the decision service records it.
func (r *ReportHookError) HookError() *decision.HookError {
	return (*decision.HookError)(r)
}

// Ack says the server took a frame that has no other answer.
type Ack struct{}

// Query is a read for the CLI: a kind and its parameters. The server
// answers with QueryResult or Error.
type Query struct {
	Kind   string            `json:"kind"`
	Params map[string]string `json:"params,omitempty"`
	Limit  int               `json:"limit,omitempty"`
}

// QueryResult carries the rows of a Query. Next is the cursor of the next
// page, empty on the last one.
type QueryResult struct {
	Rows []json.RawMessage `json:"rows"`
	Next string            `json:"next,omitempty"`
}

// SessionCost carries the cost totals that the client collected from the
// transcript of a session, for gryph cost --sync. The server stores them
// on the session of its own partition, marked as client reported.
type SessionCost struct {
	SessionID uuid.UUID         `json:"session_id"`
	Cost      *cost.SessionCost `json:"cost_totals"`
}

// ImportEvents carries up to MaxQueryItems events of one session from the
// user's own database, for gryph supervisor import. The server stores
// them in its own partition, marked imported.
type ImportEvents struct {
	SessionID uuid.UUID       `json:"session_id"`
	Events    []*events.Event `json:"events"`
}

// ImportReceipts carries up to MaxQueryItems receipts of one session from
// the user's own database, with their hashes and signatures.
type ImportReceipts struct {
	SessionID uuid.UUID             `json:"session_id"`
	Receipts  []*storage.ReceiptRow `json:"receipts"`
}

// ImportSession carries the session row, first: the events and the
// receipts of the session reference it.
type ImportSession struct {
	Session *session.Session `json:"session"`
}

// ImportResult answers an import frame with the count of rows the server
// took. A row that was already there counts as zero.
type ImportResult struct {
	Taken int `json:"taken"`
}

// Error answers a frame the server refuses. Code is one of the Code
// constants. A client maps a code it does not know like CodeInternal.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Validate implements Body.
func (h *Hello) Validate() error {
	if h.Proto <= 0 {
		return fmt.Errorf("%w: hello without a protocol version", ErrInvalid)
	}
	if err := boundString("client_version", h.ClientVersion, MaxVersion); err != nil {
		return err
	}
	if len(h.Caps) > MaxCaps {
		return fmt.Errorf("%w: more than %d caps", ErrInvalid, MaxCaps)
	}
	for _, c := range h.Caps {
		if err := boundString("cap", c, MaxName); err != nil {
			return err
		}
	}
	return nil
}

// Validate implements Body.
func (w *Welcome) Validate() error {
	if w.Proto <= 0 {
		return fmt.Errorf("%w: welcome without a protocol version", ErrInvalid)
	}
	if err := boundString("server_version", w.ServerVersion, MaxVersion); err != nil {
		return err
	}
	return boundString("mode", w.Mode, MaxName)
}

// Validate implements Body.
func (h *Handle) Validate() error {
	if h.Agent == "" || h.HookType == "" {
		return fmt.Errorf("%w: handle without an agent or a hook type", ErrInvalid)
	}
	if err := boundString("agent", h.Agent, MaxName); err != nil {
		return err
	}
	if err := boundString("hook_type", h.HookType, MaxName); err != nil {
		return err
	}
	if len(h.RawPayload) > MaxRawPayload {
		return fmt.Errorf("%w: raw_payload above %d bytes", ErrInvalid, MaxRawPayload)
	}
	if err := boundString("project_claim.name", h.Project.Name, MaxProject); err != nil {
		return err
	}
	if h.Cost != nil {
		return validateCost(h.Cost)
	}
	return nil
}

// validateCost bounds the strings of cost totals.
func validateCost(c *cost.SessionCost) error {
	if err := boundString("cost_totals.currency", c.Currency, MaxName); err != nil {
		return err
	}
	if err := boundString("cost_totals.source", string(c.Source), MaxName); err != nil {
		return err
	}
	if len(c.Models) > MaxQueryItems {
		return fmt.Errorf("%w: more than %d models in cost_totals", ErrInvalid, MaxQueryItems)
	}
	for _, m := range c.Models {
		if err := boundString("cost_totals.models.model", m.Model, MaxName); err != nil {
			return err
		}
	}
	return nil
}

// Validate implements Body.
func (d *Decision) Validate() error {
	if err := boundString("decision", string(d.Decision), MaxName); err != nil {
		return err
	}
	if err := boundString("reason", d.Reason, MaxText); err != nil {
		return err
	}
	return boundString("guidance", d.Guidance, MaxText)
}

// Validate implements Body.
func (p *Prompt) Validate() error {
	if p.Nonce == "" {
		return fmt.Errorf("%w: prompt without a nonce", ErrInvalid)
	}
	if err := boundString("nonce", p.Nonce, MaxName); err != nil {
		return err
	}
	if err := boundString("action_digest", p.ActionDigest, MaxDigest); err != nil {
		return err
	}
	if p.Deadline.IsZero() {
		return fmt.Errorf("%w: prompt without a deadline", ErrInvalid)
	}
	return nil
}

// Validate implements Body.
func (p *PromptReply) Validate() error {
	if p.Nonce == "" {
		return fmt.Errorf("%w: prompt reply without a nonce", ErrInvalid)
	}
	if err := boundString("nonce", p.Nonce, MaxName); err != nil {
		return err
	}
	if err := boundString("decision", string(p.Decision), MaxName); err != nil {
		return err
	}
	return boundString("note", p.Note, MaxText)
}

// Validate implements Body.
func (e *ReportHookError) Validate() error {
	if err := boundString("agent", e.Agent, MaxName); err != nil {
		return err
	}
	if err := boundString("hook_type", e.HookType, MaxName); err != nil {
		return err
	}
	if err := boundString("message", e.Message, MaxText); err != nil {
		return err
	}
	if len(e.RawEvent) > MaxRawEvent {
		return fmt.Errorf("%w: raw_event above %d bytes", ErrInvalid, MaxRawEvent)
	}
	if e.RawSize < 0 {
		return fmt.Errorf("%w: negative raw_size", ErrInvalid)
	}
	return nil
}

// Validate implements Body.
func (q *Query) Validate() error {
	if q.Kind == "" {
		return fmt.Errorf("%w: query without a kind", ErrInvalid)
	}
	if err := boundString("kind", q.Kind, MaxName); err != nil {
		return err
	}
	if len(q.Params) > MaxQueryItems {
		return fmt.Errorf("%w: more than %d query parameters", ErrInvalid, MaxQueryItems)
	}
	for k, v := range q.Params {
		if err := boundString("param", k, MaxName); err != nil {
			return err
		}
		if err := boundString("param "+k, v, MaxQueryParam); err != nil {
			return err
		}
	}
	if q.Limit < 0 {
		return fmt.Errorf("%w: negative limit", ErrInvalid)
	}
	return nil
}

// Validate implements Body.
func (c *SessionCost) Validate() error {
	if c.SessionID == uuid.Nil {
		return fmt.Errorf("%w: session_cost without a session", ErrInvalid)
	}
	if c.Cost == nil {
		return fmt.Errorf("%w: session_cost without totals", ErrInvalid)
	}
	return validateCost(c.Cost)
}

// Validate implements Body.
func (i *ImportEvents) Validate() error {
	if i.SessionID == uuid.Nil {
		return fmt.Errorf("%w: import_events without a session", ErrInvalid)
	}
	if len(i.Events) > MaxQueryItems {
		return fmt.Errorf("%w: more than %d events in import_events", ErrInvalid, MaxQueryItems)
	}
	for _, e := range i.Events {
		if e == nil {
			return fmt.Errorf("%w: import_events with an empty event", ErrInvalid)
		}
	}
	return nil
}

// Validate implements Body.
func (i *ImportReceipts) Validate() error {
	if i.SessionID == uuid.Nil {
		return fmt.Errorf("%w: import_receipts without a session", ErrInvalid)
	}
	if len(i.Receipts) > MaxQueryItems {
		return fmt.Errorf("%w: more than %d receipts in import_receipts", ErrInvalid, MaxQueryItems)
	}
	for _, r := range i.Receipts {
		if r == nil {
			return fmt.Errorf("%w: import_receipts with an empty receipt", ErrInvalid)
		}
	}
	return nil
}

// Validate implements Body.
func (i *ImportSession) Validate() error {
	if i.Session == nil || i.Session.ID == uuid.Nil {
		return fmt.Errorf("%w: import_session without a session", ErrInvalid)
	}
	return boundString("session.agent_name", i.Session.AgentName, MaxName)
}

// Validate implements Body.
func (*ImportResult) Validate() error { return nil }

// Validate implements Body.
func (q *QueryResult) Validate() error {
	if len(q.Rows) > MaxQueryItems {
		return fmt.Errorf("%w: more than %d rows", ErrInvalid, MaxQueryItems)
	}
	return boundString("next", q.Next, MaxQueryParam)
}

// Validate implements Body.
func (e *Error) Validate() error {
	if e.Code == "" {
		return fmt.Errorf("%w: error without a code", ErrInvalid)
	}
	if err := boundString("code", e.Code, MaxName); err != nil {
		return err
	}
	return boundString("message", e.Message, MaxText)
}

// Validate implements Body.
func (*Ack) Validate() error { return nil }

// Body is a decoded frame body. Validate checks its bounds.
type Body interface {
	Validate() error
}

// Decode turns a frame into its body and checks the bounds. It returns
// ErrUnsupported for a type this binary does not know and ErrInvalid for a
// body that does not decode or fails a bound.
func Decode(f *Frame) (Body, error) {
	var body Body
	switch f.Type {
	case TypeHello:
		body = &Hello{}
	case TypeWelcome:
		body = &Welcome{}
	case TypeHandle:
		body = &Handle{}
	case TypeDecision:
		body = &Decision{}
	case TypePrompt:
		body = &Prompt{}
	case TypePromptReply:
		body = &PromptReply{}
	case TypeReportHookError:
		body = &ReportHookError{}
	case TypeAck:
		body = &Ack{}
	case TypeQuery:
		body = &Query{}
	case TypeQueryResult:
		body = &QueryResult{}
	case TypeSessionCost:
		body = &SessionCost{}
	case TypeImportEvents:
		body = &ImportEvents{}
	case TypeImportReceipts:
		body = &ImportReceipts{}
	case TypeImportSession:
		body = &ImportSession{}
	case TypeImportResult:
		body = &ImportResult{}
	case TypeError:
		body = &Error{}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, f.Type)
	}
	if len(f.Body) > 0 {
		if err := json.Unmarshal(f.Body, body); err != nil {
			return nil, fmt.Errorf("%w: %s body: %v", ErrInvalid, f.Type, err)
		}
	}
	if err := body.Validate(); err != nil {
		return nil, err
	}
	return body, nil
}

func boundString(field, s string, limit int) error {
	if len(s) > limit {
		return fmt.Errorf("%w: %s above %d bytes", ErrInvalid, field, limit)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s is not UTF-8", ErrInvalid, field)
	}
	return nil
}
