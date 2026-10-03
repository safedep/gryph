package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/aarmapprovalgrant"
	"github.com/safedep/gryph/storage/ent/aarmapprovalrequest"
)

// The states of an approval request. A request leaves pending once, and
// a later answer never moves it again.
const (
	ApprovalRequestPending  = "pending"
	ApprovalRequestApproved = "approved"
	ApprovalRequestDenied   = "denied"
	ApprovalRequestExpired  = "expired"
)

// ErrApprovalRequestDecided says a request already left pending, so a
// later answer does not apply.
var ErrApprovalRequestDecided = errors.New("storage: the approval request is already decided")

const approvalRequestListMaxLimit = 5000

// ApprovalRequestRow mirrors the aarm_approval_requests ent row.
type ApprovalRequestRow struct {
	ID              uuid.UUID
	SessionID       uuid.UUID
	ActionID        uuid.UUID
	ReceiptSequence int64
	ActionDigest    string
	RuleIDs         []string
	Requester       string
	Host            string
	Agent           string
	Summary         string
	Project         string
	MinAssurance    string
	State           string
	RequestedAt     time.Time
	ExpiresAt       time.Time
	Inline          bool
	Review          bool
	RequesterAudit  string
	DecidedAt       *time.Time
	Channel         string
	Assurance       string
	Approver        string
	PeerTrust       string
	Note            string
	Scope           string
	NotifiedAt      *time.Time
}

// ApprovalRequestFilter narrows QueryApprovalRequests.
type ApprovalRequestFilter struct {
	SessionID *uuid.UUID
	State     string
	// ExpiredBefore keeps rows whose expires_at is before the time.
	ExpiredBefore *time.Time
	// Unnotified keeps decided rows that no later hook reported yet.
	Unnotified bool
	// AllAccounts asks the decision service for the requests of every
	// account. The service honors it for an approver and ignores it for
	// anyone else. A store of one account ignores it.
	AllAccounts bool
	Limit       int
}

// ApprovalResolution is the answer that closes a request.
type ApprovalResolution struct {
	State     string
	DecidedAt time.Time
	Channel   string
	Assurance string
	Approver  string
	PeerTrust string
	Note      string
	Scope     string
}

// ApprovalGrantRow mirrors the aarm_approval_grants ent row.
type ApprovalGrantRow struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	SessionID    uuid.UUID
	ActionDigest string
	Scope        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	UsedAt       *time.Time
	Uses         int
	Approver     string
	Assurance    string
	Channel      string
}

// ApprovalStore keeps the requests and the grants of the decision
// service. A request is decided once: ResolveApprovalRequest applies only
// to a pending row and reports ErrApprovalRequestDecided otherwise, so
// the first answer wins by the clock of the store.
type ApprovalStore interface {
	InsertApprovalRequest(ctx context.Context, row *ApprovalRequestRow) error
	GetApprovalRequest(ctx context.Context, id uuid.UUID) (*ApprovalRequestRow, error)
	GetApprovalRequestByPrefix(ctx context.Context, prefix string) (*ApprovalRequestRow, error)
	QueryApprovalRequests(ctx context.Context, filter *ApprovalRequestFilter) ([]*ApprovalRequestRow, error)
	ResolveApprovalRequest(ctx context.Context, id uuid.UUID, res ApprovalResolution) error
	MarkApprovalRequestNotified(ctx context.Context, id uuid.UUID, at time.Time) error

	InsertApprovalGrant(ctx context.Context, row *ApprovalGrantRow) error
	// MatchApprovalGrant returns the grant that a retry of the action can
	// use, or nil. A once grant matches while unused, a session grant for
	// the same session, a window grant for the account. Every one matches
	// until it expires.
	MatchApprovalGrant(ctx context.Context, sessionID uuid.UUID, digest string, now time.Time) (*ApprovalGrantRow, error)
	UseApprovalGrant(ctx context.Context, id uuid.UUID, at time.Time) error
}

// InsertApprovalRequest stores a new request. A zero id and a zero
// requested_at take fresh values.
func (s *SQLiteStore) InsertApprovalRequest(ctx context.Context, row *ApprovalRequestRow) error {
	if row == nil {
		return fmt.Errorf("storage: InsertApprovalRequest: nil row")
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.SessionID == uuid.Nil {
		return fmt.Errorf("storage: InsertApprovalRequest: nil session id")
	}
	if row.RequestedAt.IsZero() {
		row.RequestedAt = time.Now().UTC()
	}
	if row.State == "" {
		row.State = ApprovalRequestPending
	}
	create := s.client.AarmApprovalRequest.Create().
		SetID(row.ID).
		SetSessionID(row.SessionID).
		SetActionID(row.ActionID).
		SetReceiptSequence(row.ReceiptSequence).
		SetActionDigest(row.ActionDigest).
		SetRuleIds(row.RuleIDs).
		SetRequester(row.Requester).
		SetHost(row.Host).
		SetAgent(row.Agent).
		SetSummary(row.Summary).
		SetProject(row.Project).
		SetMinAssurance(row.MinAssurance).
		SetState(aarmapprovalrequest.State(row.State)).
		SetRequestedAt(row.RequestedAt).
		SetExpiresAt(row.ExpiresAt).
		SetInline(row.Inline).
		SetReview(row.Review).
		SetRequesterAudit(row.RequesterAudit).
		SetChannel(row.Channel).
		SetAssurance(row.Assurance).
		SetApprover(row.Approver).
		SetPeerTrust(row.PeerTrust).
		SetNote(row.Note).
		SetScope(row.Scope)
	if row.DecidedAt != nil {
		create.SetDecidedAt(*row.DecidedAt)
	}
	if row.NotifiedAt != nil {
		create.SetNotifiedAt(*row.NotifiedAt)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("storage: insert approval request: %w", err)
	}
	return nil
}

// GetApprovalRequest returns the request with id, or nil.
func (s *SQLiteStore) GetApprovalRequest(ctx context.Context, id uuid.UUID) (*ApprovalRequestRow, error) {
	row, err := s.client.AarmApprovalRequest.Query().Where(aarmapprovalrequest.IDEQ(id)).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get approval request: %w", err)
	}
	return entToApprovalRequest(row), nil
}

// GetApprovalRequestByPrefix returns the one request whose id starts with
// prefix, or nil. More than one match is an error.
func (s *SQLiteStore) GetApprovalRequestByPrefix(ctx context.Context, prefix string) (*ApprovalRequestRow, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return nil, fmt.Errorf("storage: GetApprovalRequestByPrefix: empty prefix")
	}
	if id, err := uuid.Parse(trimmed); err == nil {
		return s.GetApprovalRequest(ctx, id)
	}
	rows, err := s.client.AarmApprovalRequest.Query().
		Where(func(sel *entsql.Selector) {
			sel.Where(entsql.Like(aarmapprovalrequest.FieldID, trimmed+"%"))
		}).
		Limit(2).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: get approval request by prefix: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("storage: approval request prefix %q is ambiguous", trimmed)
	}
	return entToApprovalRequest(rows[0]), nil
}

// QueryApprovalRequests returns the requests that match filter, newest
// first.
func (s *SQLiteStore) QueryApprovalRequests(ctx context.Context, filter *ApprovalRequestFilter) ([]*ApprovalRequestRow, error) {
	if filter == nil {
		filter = &ApprovalRequestFilter{}
	}
	q := s.client.AarmApprovalRequest.Query()
	if filter.SessionID != nil {
		q.Where(aarmapprovalrequest.SessionIDEQ(*filter.SessionID))
	}
	if filter.State != "" {
		q.Where(aarmapprovalrequest.StateEQ(aarmapprovalrequest.State(filter.State)))
	}
	if filter.ExpiredBefore != nil {
		q.Where(aarmapprovalrequest.ExpiresAtLT(*filter.ExpiredBefore))
	}
	if filter.Unnotified {
		q.Where(aarmapprovalrequest.StateNEQ(aarmapprovalrequest.StatePending), aarmapprovalrequest.NotifiedAtIsNil())
	}
	q.Order(aarmapprovalrequest.ByRequestedAt(entsql.OrderDesc()), aarmapprovalrequest.ByID(entsql.OrderDesc()))
	limit := filter.Limit
	if limit != -1 {
		if limit <= 0 || limit > approvalRequestListMaxLimit {
			limit = approvalRequestListMaxLimit
		}
		q.Limit(limit)
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query approval requests: %w", err)
	}
	out := make([]*ApprovalRequestRow, len(rows))
	for i, r := range rows {
		out[i] = entToApprovalRequest(r)
	}
	return out, nil
}

// ResolveApprovalRequest moves a pending request to its final state. The
// update names the pending state in its condition, so two answers never
// both apply: the second one gets ErrApprovalRequestDecided.
func (s *SQLiteStore) ResolveApprovalRequest(ctx context.Context, id uuid.UUID, res ApprovalResolution) error {
	switch res.State {
	case ApprovalRequestApproved, ApprovalRequestDenied, ApprovalRequestExpired:
	default:
		return fmt.Errorf("storage: ResolveApprovalRequest: invalid state %q", res.State)
	}
	if res.DecidedAt.IsZero() {
		res.DecidedAt = time.Now().UTC()
	}
	n, err := s.client.AarmApprovalRequest.Update().
		Where(aarmapprovalrequest.IDEQ(id), aarmapprovalrequest.StateEQ(aarmapprovalrequest.StatePending)).
		SetState(aarmapprovalrequest.State(res.State)).
		SetDecidedAt(res.DecidedAt).
		SetChannel(res.Channel).
		SetAssurance(res.Assurance).
		SetApprover(res.Approver).
		SetPeerTrust(res.PeerTrust).
		SetNote(res.Note).
		SetScope(res.Scope).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("storage: resolve approval request: %w", err)
	}
	if n == 0 {
		return ErrApprovalRequestDecided
	}
	return nil
}

// MarkApprovalRequestNotified records that a later hook reported the
// outcome to the agent.
func (s *SQLiteStore) MarkApprovalRequestNotified(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := s.client.AarmApprovalRequest.UpdateOneID(id).SetNotifiedAt(at).Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("storage: mark approval request notified: %w", err)
	}
	return nil
}

// InsertApprovalGrant stores a grant.
func (s *SQLiteStore) InsertApprovalGrant(ctx context.Context, row *ApprovalGrantRow) error {
	if row == nil {
		return fmt.Errorf("storage: InsertApprovalGrant: nil row")
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.ActionDigest == "" {
		return fmt.Errorf("storage: InsertApprovalGrant: empty action digest")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	create := s.client.AarmApprovalGrant.Create().
		SetID(row.ID).
		SetRequestID(row.RequestID).
		SetSessionID(row.SessionID).
		SetActionDigest(row.ActionDigest).
		SetScope(aarmapprovalgrant.Scope(row.Scope)).
		SetCreatedAt(row.CreatedAt).
		SetExpiresAt(row.ExpiresAt).
		SetUses(row.Uses).
		SetApprover(row.Approver).
		SetAssurance(row.Assurance).
		SetChannel(row.Channel)
	if row.UsedAt != nil {
		create.SetUsedAt(*row.UsedAt)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("storage: insert approval grant: %w", err)
	}
	return nil
}

// MatchApprovalGrant implements ApprovalStore.
func (s *SQLiteStore) MatchApprovalGrant(ctx context.Context, sessionID uuid.UUID, digest string, now time.Time) (*ApprovalGrantRow, error) {
	if digest == "" {
		return nil, nil
	}
	rows, err := s.client.AarmApprovalGrant.Query().
		Where(
			aarmapprovalgrant.ActionDigestEQ(digest),
			aarmapprovalgrant.ExpiresAtGT(now),
			aarmapprovalgrant.Or(
				aarmapprovalgrant.And(aarmapprovalgrant.ScopeEQ(aarmapprovalgrant.ScopeOnce), aarmapprovalgrant.UsedAtIsNil(), aarmapprovalgrant.SessionIDEQ(sessionID)),
				aarmapprovalgrant.And(aarmapprovalgrant.ScopeEQ(aarmapprovalgrant.ScopeSession), aarmapprovalgrant.SessionIDEQ(sessionID)),
				aarmapprovalgrant.ScopeEQ(aarmapprovalgrant.ScopeWindow),
			),
		).
		Order(aarmapprovalgrant.ByCreatedAt(entsql.OrderAsc())).
		Limit(1).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: match approval grant: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return entToApprovalGrant(rows[0]), nil
}

// UseApprovalGrant counts one use of the grant.
func (s *SQLiteStore) UseApprovalGrant(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := s.client.AarmApprovalGrant.UpdateOneID(id).SetUsedAt(at).AddUses(1).Save(ctx); err != nil {
		return fmt.Errorf("storage: use approval grant: %w", err)
	}
	return nil
}

func entToApprovalRequest(e *ent.AarmApprovalRequest) *ApprovalRequestRow {
	return &ApprovalRequestRow{
		ID:              e.ID,
		SessionID:       e.SessionID,
		ActionID:        e.ActionID,
		ReceiptSequence: e.ReceiptSequence,
		ActionDigest:    e.ActionDigest,
		RuleIDs:         e.RuleIds,
		Requester:       e.Requester,
		Host:            e.Host,
		Agent:           e.Agent,
		Summary:         e.Summary,
		Project:         e.Project,
		MinAssurance:    e.MinAssurance,
		State:           string(e.State),
		RequestedAt:     e.RequestedAt,
		ExpiresAt:       e.ExpiresAt,
		Inline:          e.Inline,
		Review:          e.Review,
		RequesterAudit:  e.RequesterAudit,
		DecidedAt:       e.DecidedAt,
		Channel:         e.Channel,
		Assurance:       e.Assurance,
		Approver:        e.Approver,
		PeerTrust:       e.PeerTrust,
		Note:            e.Note,
		Scope:           e.Scope,
		NotifiedAt:      e.NotifiedAt,
	}
}

func entToApprovalGrant(e *ent.AarmApprovalGrant) *ApprovalGrantRow {
	return &ApprovalGrantRow{
		ID:           e.ID,
		RequestID:    e.RequestID,
		SessionID:    e.SessionID,
		ActionDigest: e.ActionDigest,
		Scope:        string(e.Scope),
		CreatedAt:    e.CreatedAt,
		ExpiresAt:    e.ExpiresAt,
		UsedAt:       e.UsedAt,
		Uses:         e.Uses,
		Approver:     e.Approver,
		Assurance:    e.Assurance,
		Channel:      e.Channel,
	}
}
