package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/platform/localauth"
	"github.com/safedep/gryph/platform/peercred"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/remote"
)

const (
	remoteKindApprovalRequests        = remote.KindApprovalRequests
	remoteKindApprovalRequestByPrefix = remote.KindApprovalRequestByPrefix
	remoteParamPrefix                 = remote.ParamPrefix
	remoteParamAll                    = remote.ParamAll
)

// approverCacheTTL bounds how long the service remembers a group lookup,
// so a change of the group database reaches it without a restart.
const approverCacheTTL = 30 * time.Second

// authWait bounds how long the authority may ask the approver for the
// password. The client waits a little longer for the answer.
const authWait = 90 * time.Second

// authCache remembers whether the authority of the platform runs, for
// approverCacheTTL.
type authCache struct {
	mu        sync.Mutex
	available bool
	at        time.Time
}

// authAvailable reports whether the host can ask an approver for the
// password.
func (s *Server) authAvailable(ctx context.Context) bool {
	s.authCache.mu.Lock()
	defer s.authCache.mu.Unlock()
	if !s.authCache.at.IsZero() && time.Since(s.authCache.at) < approverCacheTTL {
		return s.authCache.available
	}
	s.authCache.available = s.auth.Available(ctx)
	s.authCache.at = time.Now()
	return s.authCache.available
}

// authenticate asks the authority for the password of the approver. It
// returns the reason the answer cannot count, or empty when it can.
func (s *Server) authenticate(ctx context.Context, peer *peercred.Peer) string {
	subject, err := localauth.SubjectOf(peer.PID)
	if err != nil {
		return "the approver process is not there to authenticate: " + err.Error()
	}
	actx, cancel := context.WithTimeout(ctx, authWait)
	defer cancel()
	res, err := s.auth.Authorize(actx, subject, localauth.ActionID, true)
	switch {
	case errors.Is(err, localauth.ErrUnavailable):
		return "the authority of the host stopped answering: " + err.Error()
	case err != nil:
		return "the authority of the host did not answer: " + err.Error()
	case res.Authorized:
		return ""
	case res.Dismissed:
		return "the approver dismissed the password prompt"
	case res.Challenge:
		return "no password was given: run the command from a session with a polkit agent, or on a terminal where pkttyagent can ask"
	}
	return "the authority of the host refused the approver"
}

// approverCache remembers who is in the approval group.
type approverCache struct {
	mu      sync.Mutex
	entries map[uint32]approverEntry
}

type approverEntry struct {
	member bool
	at     time.Time
}

// isApprover reports whether the peer uid is in the approval group of the
// managed configuration. An empty group has no members.
func (s *Server) isApprover(uid uint32) bool {
	group := s.cfg.Policy.Approval.LocalAdmin.Group
	if group == "" || !s.cfg.Policy.Approval.HasChannel(config.ApprovalChannelLocalAdmin) {
		return false
	}
	s.approvers.mu.Lock()
	defer s.approvers.mu.Unlock()
	if s.approvers.entries == nil {
		s.approvers.entries = map[uint32]approverEntry{}
	}
	if e, ok := s.approvers.entries[uid]; ok && time.Since(e.at) < approverCacheTTL {
		return e.member
	}
	member, err := account.MemberOf(strconv.FormatUint(uint64(uid), 10), group)
	if err != nil {
		log.Warnf("supervisor: approval group %s for uid %d: %v", group, uid, err)
		member = false
	}
	s.approvers.entries[uid] = approverEntry{member: member, at: time.Now()}
	return member
}

// allPartitions opens the partition of every account under the state
// directory, so an approver sees the requests of every account.
func (s *Server) allPartitions(ctx context.Context) ([]*partition, error) {
	s.mu.Lock()
	root := s.rootDir
	s.mu.Unlock()
	if root == nil {
		return nil, errors.New("supervisor: the state directory is not open")
	}
	users, err := root.OpenDir("users")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = users.Close() }()
	entries, err := users.ReadDir()
	if err != nil {
		return nil, err
	}
	var uids []uint32
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		uid, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue
		}
		uids = append(uids, uint32(uid))
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	parts := make([]*partition, 0, len(uids))
	for _, uid := range uids {
		p, err := s.partition(ctx, uid)
		if err != nil {
			log.Warnf("supervisor: partition of uid %d: %v", uid, err)
			continue
		}
		parts = append(parts, p)
	}
	return parts, nil
}

// requestsOf reads the approval requests of every account for an
// approver, else of the peer's own partition.
func (s *Server) requestsOf(ctx context.Context, own *partition, peer *peercred.Peer, filter *storage.ApprovalRequestFilter) ([]any, error) {
	if !filter.AllAccounts || !s.isApprover(peer.UID) {
		own.expireNow(ctx)
		return rowsOf(own.rt.Store.QueryApprovalRequests(ctx, filter))
	}
	parts, err := s.allPartitions(ctx)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, p := range parts {
		p.expireNow(ctx)
		rows, err := p.rt.Store.QueryApprovalRequests(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, r)
		}
	}
	return out, nil
}

// requestByPrefix finds one request by id or prefix, in every account for
// an approver, else in the peer's own partition.
func (s *Server) requestByPrefix(ctx context.Context, own *partition, peer *peercred.Peer, prefix string, all bool) (*partition, *storage.ApprovalRequestRow, error) {
	if !all || !s.isApprover(peer.UID) {
		own.expireNow(ctx)
		row, err := own.rt.Store.GetApprovalRequestByPrefix(ctx, prefix)
		return own, row, err
	}
	parts, err := s.allPartitions(ctx)
	if err != nil {
		return nil, nil, err
	}
	var (
		found *partition
		row   *storage.ApprovalRequestRow
	)
	for _, p := range parts {
		p.expireNow(ctx)
		r, err := p.rt.Store.GetApprovalRequestByPrefix(ctx, prefix)
		if err != nil {
			return nil, nil, err
		}
		if r == nil {
			continue
		}
		if row != nil {
			return nil, nil, fmt.Errorf("the request prefix %q is ambiguous", prefix)
		}
		found, row = p, r
	}
	return found, row, nil
}

// approve answers one request of another account. The peer must be in
// the approval group and must not be the account that asked. The audit
// login identity of the peer decides between local-admin and
// self-elevated. The first answer wins, and the request store records a
// later one as superseded.
func (s *Server) approve(ctx context.Context, own *partition, peer *peercred.Peer, a *ipc.Approve) (*ipc.Frame, error) {
	if !own.bucket.take(time.Now()) {
		own.recordRateLimit(ctx, "approve")
		return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
	}
	approver := requesterName(peer.UID)
	part, row, err := s.requestByPrefix(ctx, own, peer, a.RequestID.String(), true)
	if err != nil {
		return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
	}
	if row == nil {
		return ipc.ErrorFrame(ipc.CodeInvalid, "no approval request "+a.RequestID.String()), nil
	}
	refuse := func(reason string) (*ipc.Frame, error) {
		part.auditApproval(ctx, engine.SelfAuditActionApprovalRefused, row, map[string]any{"approver": approver, "reason": reason})
		return ipc.ErrorFrame(ipc.CodeUnauthorized, reason), nil
	}
	cfg := s.cfg.Policy.Approval
	if !s.isApprover(peer.UID) {
		if cfg.LocalAdmin.Group == "" || !cfg.HasChannel(config.ApprovalChannelLocalAdmin) {
			return refuse("no approval group is configured: set policy.approval.local_admin.group")
		}
		return refuse(fmt.Sprintf("%s is not a member of the approval group %s", approver, cfg.LocalAdmin.Group))
	}
	if part.uid == peer.UID {
		return refuse("the account that asked cannot answer its own request")
	}
	if anc, found := s.agentAncestor(peer); found {
		return refuse(fmt.Sprintf("an answer from under the agent process %s (pid %d) is refused", anc.name, anc.pid))
	}
	// The assurance of the answer is what the receipt records. The floor
	// of the rule compares against it, with one exception: an
	// administrator who allows self-elevated answers accepts them in
	// place of local-admin, so that floor takes them too.
	assurance := approval.AssuranceLocalAdmin
	floorAs := assurance
	if audit, ok := peer.LoginIdentity(); !ok || row.RequesterAudit == "" || audit == row.RequesterAudit {
		// The same login session asked and answers, or one side has no
		// session to compare. That is the person who asked, through
		// another account.
		if !cfg.LocalAdmin.AllowSelfElevated {
			return refuse("the answer comes from the login session that asked (self-elevated), which policy.approval.local_admin.allow_self_elevated does not allow")
		}
		assurance = approval.AssuranceSelfElevated
	}
	// A host with an authority asks the approver for the password. An
	// agent under the approver's account does not know it. The managed
	// configuration alone can waive it. A password proves a person at
	// the keyboard, so the answer is local-auth: whether the person who
	// asked may answer was decided above.
	if s.authAvailable(ctx) {
		if reason := s.authenticate(ctx, peer); reason != "" {
			if !cfg.LocalAdmin.AllowWithoutAuth {
				return refuse("the answer needs the password of the approver: " + reason)
			}
		} else {
			assurance = approval.AssuranceLocalAuth
			floorAs = assurance
		}
	}
	if min := approval.Assurance(row.MinAssurance); !floorAs.Meets(min) {
		return refuse(fmt.Sprintf("the channel %s is below the min_assurance %s of the rule", assurance, min))
	}
	scope := approval.ScopeOnce
	if a.Scope != "" {
		if scope, err = approval.ParseScope(a.Scope); err != nil {
			return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
		}
	}
	if limit := approval.Scope(cfg.MaxGrantScope); limit != "" && scope.Wider(limit) {
		return ipc.ErrorFrame(ipc.CodeInvalid, fmt.Sprintf("scope %s is wider than policy.approval.max_grant_scope %s", scope, limit)), nil
	}

	now := time.Now().UTC()
	res := storage.ApprovalResolution{
		State:     storage.ApprovalRequestDenied,
		DecidedAt: now,
		Channel:   string(assurance),
		Assurance: string(assurance),
		Approver:  approver,
		PeerTrust: approval.PeerTrustUnknown,
		Note:      a.Note,
	}
	if a.Decision == ipc.ApproveAllow {
		res.State = storage.ApprovalRequestApproved
		if !row.Review {
			res.Scope = string(scope)
		}
	}
	part.write.Lock()
	defer part.write.Unlock()
	store := part.rt.Store
	if err := store.ResolveApprovalRequest(ctx, row.ID, res); err != nil {
		if !errors.Is(err, storage.ErrApprovalRequestDecided) {
			return nil, err
		}
		current, err := store.GetApprovalRequest(ctx, row.ID)
		if err != nil || current == nil {
			return nil, fmt.Errorf("supervisor: request %s: %v", shortID(row.ID), err)
		}
		part.auditApproval(ctx, engine.SelfAuditActionApprovalSuperseded, current, map[string]any{"approver": approver, "decision": a.Decision})
		return ipc.NewFrame(ipc.TypeApproveResult, ipc.ApproveResult{
			RequestID: row.ID,
			State:     "superseded",
			Note:      fmt.Sprintf("the request is already %s by %s", current.State, current.Approver),
		})
	}

	out := ipc.ApproveResult{RequestID: row.ID, State: res.State, Assurance: string(assurance)}
	outcome := &approval.Outcome{Approver: approver, Channel: res.Channel, Assurance: assurance, PeerTrust: res.PeerTrust, RequestID: row.ID, Note: a.Note}
	decisionValue := receipt.DecisionDenied
	audit := engine.SelfAuditActionApprovalDenied
	if res.State == storage.ApprovalRequestApproved {
		decisionValue = receipt.DecisionApproved
		audit = engine.SelfAuditActionApprovalGranted
		if !row.Review {
			grant := &storage.ApprovalGrantRow{
				RequestID:    row.ID,
				SessionID:    row.SessionID,
				ActionDigest: row.ActionDigest,
				Scope:        string(scope),
				CreatedAt:    now,
				ExpiresAt:    now.Add(grantTTL(cfg)),
				Approver:     approver,
				Assurance:    string(assurance),
				Channel:      res.Channel,
			}
			if err := store.InsertApprovalGrant(ctx, grant); err != nil {
				return nil, err
			}
			out.GrantID = grant.ID.String()
			outcome.GrantID = grant.ID
			outcome.Scope = scope
		}
	}
	if err := store.UpdateReceiptDecision(ctx, row.SessionID, row.ReceiptSequence, decisionValue, "", a.Note); err != nil {
		log.Warnf("supervisor: receipt of request %s: %v", shortID(row.ID), err)
	}
	if err := store.UpdateReceiptApproval(ctx, row.SessionID, row.ReceiptSequence, outcome.Meta()); err != nil {
		log.Warnf("supervisor: receipt of request %s: %v", shortID(row.ID), err)
	}
	details := outcome.Meta()
	details["decision"] = a.Decision
	part.auditApproval(ctx, audit, row, details)
	return ipc.NewFrame(ipc.TypeApproveResult, out)
}

func grantTTL(cfg config.ApprovalConfig) time.Duration {
	if cfg.GrantTTL > 0 {
		return cfg.GrantTTL
	}
	return config.DefaultApprovalGrantTTL
}

// auditApproval writes one self-audit row of the requester's partition
// for an answer, a refusal or a superseded answer.
func (p *partition) auditApproval(ctx context.Context, action string, row *storage.ApprovalRequestRow, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	details["request_id"] = row.ID.String()
	details["session_id"] = row.SessionID.String()
	details["action_id"] = row.ActionID.String()
	result := engine.SelfAuditResultSuccess
	if action != engine.SelfAuditActionApprovalGranted {
		result = engine.SelfAuditResultSkipped
	}
	if err := engine.LogSelfAudit(ctx, p.rt.Store, action, row.Agent, details, result, ""); err != nil {
		log.Warnf("supervisor: audit of request %s: %v", shortID(row.ID), err)
	}
}

// approverRead answers the approval kinds for a query, across every
// account when the peer is an approver and the query asks for it.
func (s *Server) approverRead(ctx context.Context, own *partition, peer *peercred.Peer, q *ipc.Query) ([]any, bool, error) {
	switch q.Kind {
	case remoteKindApprovalRequests:
		filter, err := filterParam[storage.ApprovalRequestFilter](q)
		if err != nil {
			return nil, true, err
		}
		rows, err := s.requestsOf(ctx, own, peer, filter)
		return rows, true, err
	case remoteKindApprovalRequestByPrefix:
		_, row, err := s.requestByPrefix(ctx, own, peer, q.Params[remoteParamPrefix], q.Params[remoteParamAll] != "")
		if err != nil {
			return nil, true, err
		}
		rows, err := rowOf(row, nil)
		return rows, true, err
	}
	return nil, false, nil
}
