package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/storage"
)

// promptGrace is what the server waits past the deadline of a prompt for
// the reply of a client that answers none at its own deadline.
const promptGrace = time.Second

// expiryApprover names the service as the resolver of an expired request.
const expiryApprover = "system:expiry"

// approver is the approval service of one partition. It keeps the request
// store of the account, decides every request once, and offers the inline
// prompt on the connection of the hook when the rule accepts the
// terminal of the developer. It never stores a grant for an answer from
// that terminal.
type approver struct {
	p         *partition
	cfg       config.ApprovalConfig
	host      string
	requester string
}

func newApprover(p *partition, cfg config.ApprovalConfig) *approver {
	host, _ := os.Hostname()
	return &approver{p: p, cfg: cfg, host: host, requester: requesterName(p.uid)}
}

// requesterName is the uid and the account name, when the host knows it.
func requesterName(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	acct, err := account.LookupID(id)
	if err != nil || acct.Name == "" {
		return id
	}
	return id + " (" + acct.Name + ")"
}

// floor is the lowest assurance the request accepts.
func (a *approver) floor(r *approval.Request) approval.Assurance {
	if r.MinAssurance != "" {
		return r.MinAssurance
	}
	if min, err := approval.ParseAssurance(a.cfg.MinAssurance); err == nil {
		return min
	}
	return approval.AssuranceLocalAdmin
}

// channels returns the configured channels that meet min.
func (a *approver) channels(min approval.Assurance) []string {
	var out []string
	for _, c := range a.cfg.Channels {
		if approval.Assurance(c).Meets(min) {
			out = append(out, c)
		}
	}
	return out
}

func (a *approver) requestTTL() time.Duration {
	if a.cfg.RequestTTL > 0 {
		return a.cfg.RequestTTL
	}
	return config.DefaultApprovalRequestTTL
}

func (a *approver) inlineWait() time.Duration {
	if a.cfg.InlineWait > 0 {
		return a.cfg.InlineWait
	}
	return config.DefaultApprovalInlineWait
}

// Request implements approval.Service. A grant for the action answers at
// once. Else the request goes into the store, and the inline prompt runs
// when the floor accepts the terminal and the client can wait. Anything
// else leaves the request pending for a later channel.
func (a *approver) Request(ctx context.Context, r *approval.Request) (*approval.Outcome, error) {
	if r == nil {
		return nil, errors.New("supervisor: nil approval request")
	}
	store := a.p.rt.Store
	now := time.Now().UTC()

	if g, err := store.MatchApprovalGrant(ctx, r.SessionID, r.Digest, now); err != nil {
		log.Warnf("supervisor: grant lookup: %v", err)
	} else if g != nil {
		// A grant that cannot be marked used does not approve: a once
		// grant would then answer the same action again.
		if err := store.UseApprovalGrant(ctx, g.ID, now); err != nil {
			return nil, fmt.Errorf("supervisor: use grant %s: %w", shortID(g.ID), err)
		}
		return &approval.Outcome{
			Decision:  approval.DecisionApprove,
			Approver:  g.Approver,
			Note:      fmt.Sprintf("grant %s (%s)", shortID(g.ID), g.Scope),
			DecidedAt: now,
			Channel:   g.Channel,
			Assurance: approval.Assurance(g.Assurance),
			PeerTrust: approval.PeerTrustUnknown,
			RequestID: g.RequestID,
			GrantID:   g.ID,
			Scope:     approval.Scope(g.Scope),
		}, nil
	}

	min := a.floor(r)
	// A connection the service does not trust gets no prompt: only a
	// channel above the developer's terminal answers it.
	lowTrust := r.PeerTrust == approval.PeerTrustLow
	if lowTrust && !min.Meets(approval.AssuranceLocalAdmin) {
		min = approval.AssuranceLocalAdmin
	}
	channels := a.channels(min)
	pr := prompterFrom(ctx)
	// A hook after the action cannot stop it. The request is a review
	// item: an approver sees it, and nobody waits.
	review := r.Action != nil && r.Action.Phase != model.PhasePre
	inline := !review && !lowTrust && pr != nil && pr.wait > 0 && a.cfg.HasChannel(config.ApprovalChannelSameUserTTY) && approval.AssuranceSameUserTTY.Meets(min)
	row := &storage.ApprovalRequestRow{
		SessionID:       r.SessionID,
		ActionID:        r.ActionID,
		ReceiptSequence: r.ReceiptSequence,
		ActionDigest:    r.Digest,
		Requester:       a.requester,
		Host:            a.host,
		Summary:         approval.Summary(r.Action),
		MinAssurance:    string(min),
		State:           storage.ApprovalRequestPending,
		RequestedAt:     now,
		ExpiresAt:       now.Add(a.requestTTL()),
		Inline:          inline,
		Review:          review,
	}
	if pr != nil {
		row.RequesterAudit = pr.audit
	}
	row.RequesterTrust = r.PeerTrust
	if r.Action != nil {
		row.Agent = r.Action.Agent
		row.Project = r.Action.Project
	}
	if r.Rule != nil {
		row.RuleIDs = r.Rule.MatchedRuleIDs
	}
	if err := store.InsertApprovalRequest(ctx, row); err != nil {
		return nil, fmt.Errorf("supervisor: store the approval request: %w", err)
	}

	if review {
		return a.review(row), nil
	}
	if lowTrust {
		return a.pending(row, "The connection is not under the agent of the session, so only an approver answers."), nil
	}
	if len(channels) == 0 {
		return a.pending(row, fmt.Sprintf("No approval channel meets min_assurance %s.", min)), nil
	}
	if !inline {
		return a.pending(row, ""), nil
	}

	wait := min_(pr.wait, a.inlineWait())
	deadline := now.Add(wait)
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	view := approval.ViewOf(r)
	view.RequestID = row.ID.String()
	view.Wait = wait
	prompt := ipc.Prompt{Nonce: nonce, ActionDigest: r.Digest, Deadline: deadline, RequestID: row.ID.String(), View: view}

	// The partition takes no writes while the human reads the prompt, so
	// the other tool calls of the session go on. The outcome below takes
	// the lock again.
	a.p.write.Unlock()
	reply, err := pr.ask(&prompt, deadline.Add(promptGrace))
	a.p.write.Lock()
	if err != nil {
		log.Debugf("supervisor: inline prompt of request %s: %v", shortID(row.ID), err)
		return a.pending(row, ""), nil
	}

	decidedAt := time.Now().UTC()
	res := storage.ApprovalResolution{
		DecidedAt: decidedAt,
		Channel:   config.ApprovalChannelSameUserTTY,
		Assurance: string(approval.AssuranceSameUserTTY),
		Approver:  a.requester + " on the terminal",
		PeerTrust: trustOr(r.PeerTrust),
		Note:      reply.Note,
	}
	switch reply.Decision {
	case ipc.PromptApprove:
		res.State = storage.ApprovalRequestApproved
	case ipc.PromptDeny:
		res.State = storage.ApprovalRequestDenied
	default:
		return a.pending(row, ""), nil
	}
	if err := store.ResolveApprovalRequest(ctx, row.ID, res); err != nil {
		if errors.Is(err, storage.ErrApprovalRequestDecided) {
			// The request expired during the wait. The answer came too
			// late to count.
			return a.pending(row, ""), nil
		}
		return nil, fmt.Errorf("supervisor: resolve the approval request: %w", err)
	}
	// The hook that asked carries the outcome, so no later hook repeats it.
	if err := store.MarkApprovalRequestNotified(ctx, row.ID, decidedAt); err != nil {
		log.Warnf("supervisor: %v", err)
	}
	out := &approval.Outcome{
		Decision:  approval.DecisionDeny,
		Approver:  res.Approver,
		Note:      reply.Note,
		DecidedAt: decidedAt,
		Channel:   res.Channel,
		Assurance: approval.AssuranceSameUserTTY,
		PeerTrust: res.PeerTrust,
		RequestID: row.ID,
	}
	if res.State == storage.ApprovalRequestApproved {
		out.Decision = approval.DecisionApprove
	}
	return out, nil
}

// pending is the outcome of a request that waits. The note is what the
// agent and the user read: it names the request, tells the agent to stop,
// and names the command that shows the state.
func (a *approver) pending(row *storage.ApprovalRequestRow, detail string) *approval.Outcome {
	id := shortID(row.ID)
	note := fmt.Sprintf("This action needs approval. Request %s is pending. %sTell the user. Do not retry until the user confirms that it is approved. Check status: gryph policy approve show %s", id, detail+spaceIf(detail), id)
	return &approval.Outcome{
		Decision:  approval.DecisionPending,
		Approver:  "",
		Note:      note,
		DecidedAt: row.RequestedAt,
		PeerTrust: trustOr(row.RequesterTrust),
		RequestID: row.ID,
	}
}

// trustOr returns the trust, or unknown when the connection has none.
func trustOr(trust string) string {
	if trust == "" {
		return approval.PeerTrustUnknown
	}
	return trust
}

// review is the outcome of a request from a hook after the action. The
// action ran, so the note tells the agent what an approver will see.
func (a *approver) review(row *storage.ApprovalRequestRow) *approval.Outcome {
	id := shortID(row.ID)
	return &approval.Outcome{
		Decision:  approval.DecisionPending,
		Note:      fmt.Sprintf("This action needs approval, and it ran before the rule could stop it. Request %s is a review item for an approver. Tell the user.", id),
		DecidedAt: row.RequestedAt,
		PeerTrust: trustOr(row.RequesterTrust),
		RequestID: row.ID,
	}
}

func spaceIf(s string) string {
	if s == "" {
		return ""
	}
	return " "
}

func min_(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("supervisor: nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func shortID(id uuid.UUID) string {
	return id.String()[:8]
}

// expireRequests closes every pending request of the partition whose time
// is up. An expiry is a deny: the receipt gets approval_timeout, and the
// next hook of the session reports it. The caller holds the write lock.
func (p *partition) expireRequests(ctx context.Context, now time.Time) {
	store := p.rt.Store
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending, ExpiredBefore: &now, Limit: -1})
	if err != nil {
		log.Warnf("supervisor: expired approval requests of uid %d: %v", p.uid, err)
		return
	}
	for _, row := range rows {
		note := fmt.Sprintf("Request %s expired without an answer.", shortID(row.ID))
		res := storage.ApprovalResolution{
			State:     storage.ApprovalRequestExpired,
			DecidedAt: now,
			Approver:  expiryApprover,
			PeerTrust: approval.PeerTrustUnknown,
			Note:      note,
		}
		if err := store.ResolveApprovalRequest(ctx, row.ID, res); err != nil {
			if !errors.Is(err, storage.ErrApprovalRequestDecided) {
				log.Warnf("supervisor: expire approval request %s: %v", shortID(row.ID), err)
			}
			continue
		}
		if err := store.UpdateReceiptDecision(ctx, row.SessionID, row.ReceiptSequence, receipt.DecisionApprovalTimeout, "blocked", note); err != nil {
			log.Warnf("supervisor: receipt of request %s: %v", shortID(row.ID), err)
		}
		meta := (&approval.Outcome{Approver: expiryApprover, PeerTrust: approval.PeerTrustUnknown, RequestID: row.ID}).Meta()
		if err := store.UpdateReceiptApproval(ctx, row.SessionID, row.ReceiptSequence, meta); err != nil {
			log.Warnf("supervisor: receipt of request %s: %v", shortID(row.ID), err)
		}
		details := map[string]any{"session_id": row.SessionID.String(), "action_id": row.ActionID.String(), "request_id": row.ID.String(), "expires_at": row.ExpiresAt.Format(time.RFC3339), "approver": expiryApprover}
		if err := engine.LogSelfAudit(ctx, store, engine.SelfAuditActionApprovalTimeout, row.Agent, details, engine.SelfAuditResultSkipped, ""); err != nil {
			log.Warnf("supervisor: audit of request %s: %v", shortID(row.ID), err)
		}
	}
}

// notices returns the outcomes of the session's requests that no hook
// reported yet, one line each, and marks them reported. The caller holds
// the write lock.
func (p *partition) notices(ctx context.Context, sessionID uuid.UUID, now time.Time) []string {
	store := p.rt.Store
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{SessionID: &sessionID, Unnotified: true, Limit: -1})
	if err != nil {
		log.Warnf("supervisor: approval notices of uid %d: %v", p.uid, err)
		return nil
	}
	var out []string
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		var line string
		switch row.State {
		case storage.ApprovalRequestApproved:
			line = fmt.Sprintf("Request %s was approved. You can retry.", shortID(row.ID))
		case storage.ApprovalRequestDenied:
			line = fmt.Sprintf("Request %s was denied.", shortID(row.ID))
			if row.Note != "" {
				line = fmt.Sprintf("Request %s was denied: %s", shortID(row.ID), row.Note)
			}
		case storage.ApprovalRequestExpired:
			line = fmt.Sprintf("Request %s expired without an answer.", shortID(row.ID))
		default:
			continue
		}
		if err := store.MarkApprovalRequestNotified(ctx, row.ID, now); err != nil {
			log.Warnf("supervisor: %v", err)
			continue
		}
		out = append(out, line)
	}
	return out
}

// withNotices adds the notices to the response the agent reads. An allow
// becomes guidance, so the agent sees the lines.
func withNotices(resp *ipc.Decision, lines []string) {
	if len(lines) == 0 {
		return
	}
	text := strings.Join(lines, " ")
	switch {
	case resp.Reason != "":
		resp.Reason += " " + text
	case resp.Guidance != "":
		resp.Guidance += " " + text
	default:
		resp.Decision = "guidance"
		resp.Guidance = text
	}
}

// expireNow closes the overdue requests of the partition, under the write
// lock, so a read sees the state the clock gives.
func (p *partition) expireNow(ctx context.Context) {
	p.write.Lock()
	defer p.write.Unlock()
	p.expireRequests(ctx, time.Now().UTC())
}

// expireRequests closes the overdue requests of every open partition.
func (s *Server) expireRequests(ctx context.Context) {
	s.mu.Lock()
	parts := make([]*partition, 0, len(s.partitions))
	for _, p := range s.partitions {
		parts = append(parts, p)
	}
	s.mu.Unlock()
	for _, p := range parts {
		p.expireNow(ctx)
	}
}
