//go:build linux || darwin

package supervisor

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The test runs as one account, so the server cannot see another peer.
// The requester check and the group check are what one account can
// exercise: an answer from the account that asked is refused, a member of
// a group the account is not in is refused, and with the account in the
// group a request of another partition resolves.
func ownGroup(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	require.NoError(t, err)
	g, err := user.LookupGroupId(u.Gid)
	require.NoError(t, err)
	return g.Name
}

func TestApproval_RequesterCannotAnswer(t *testing.T) {
	cfg := approvalConfig()
	cfg.Policy.Approval.LocalAdmin.Group = ownGroup(t)
	cfg.Policy.Approval.LocalAdmin.AllowSelfElevated = true
	_, sock, store := startApprovalServer(t, cfg)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-1"))}, nil)
	require.NoError(t, err)
	require.Equal(t, "block", string(d.Decision))
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	_, err = client.Approve(ctx, ipc.Approve{RequestID: rows[0].ID, Decision: ipc.ApproveAllow})
	var se *ipc.ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeUnauthorized, se.Code)
	assert.Contains(t, se.Message, "the account that asked cannot answer")
	audits, err := store.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: "approval_refused"})
	require.NoError(t, err)
	assert.Len(t, audits, 1)
}

func TestApproval_NotAMemberIsRefused(t *testing.T) {
	cfg := approvalConfig()
	cfg.Policy.Approval.LocalAdmin.Group = "gryph-acceptance-no-such-group"
	_, sock, store := startApprovalServer(t, cfg)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	_, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-1"))}, nil)
	require.NoError(t, err)
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, err = client.Approve(ctx, ipc.Approve{RequestID: rows[0].ID, Decision: ipc.ApproveDeny})
	var se *ipc.ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeUnauthorized, se.Code)
	assert.Contains(t, se.Message, "not a member of the approval group")

	// An unknown request is invalid, not unauthorized.
	_, err = client.Approve(ctx, ipc.Approve{RequestID: rows[0].ID, Decision: "maybe"})
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeInvalid, se.Code)
}

// seedOtherPartition puts a pending request of another account under the
// state directory, as a hook of that account would have.
func seedOtherPartition(t *testing.T, state string, uid int, review bool) (*storage.SQLiteStore, *storage.ApprovalRequestRow) {
	t.Helper()
	dir := state + "/users/" + strconv.Itoa(uid)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	other, err := storage.NewSQLiteStore(dir + "/audit.db")
	require.NoError(t, err)
	require.NoError(t, other.Init(context.Background()))
	t.Cleanup(func() { _ = other.Close() })
	ctx := context.Background()
	sessionID := [16]byte{1}
	_, err = other.RecordReceiptInTx(ctx, sessionID, func(_ *storage.ReceiptRow) (*storage.ReceiptRow, error) {
		return &storage.ReceiptRow{ID: [16]byte{2}, SessionID: sessionID, Sequence: 1, RecordedAt: time.Now(), ActionType: "command_exec", Decision: "escalate", Hash: make([]byte, 32)}, nil
	})
	require.NoError(t, err)
	row := &storage.ApprovalRequestRow{SessionID: sessionID, ActionID: [16]byte{3}, ReceiptSequence: 1, ActionDigest: "sha256:deploy", RuleIDs: []string{"escalate-deploy"}, Requester: strconv.Itoa(uid) + " (other)", Agent: "claude-code", Summary: "command_exec: make deploy", MinAssurance: "local-admin", ExpiresAt: time.Now().Add(time.Hour), Review: review}
	require.NoError(t, other.InsertApprovalRequest(ctx, row))
	return other, row
}

func TestApproval_MemberAnswersAnotherAccount(t *testing.T) {
	cfg := approvalConfig()
	cfg.Policy.Approval.LocalAdmin.Group = ownGroup(t)
	cfg.Policy.Approval.LocalAdmin.AllowSelfElevated = true
	cfg.Policy.Approval.MaxGrantScope = config.ApprovalScopeSession
	srv, sock, _ := startApprovalServer(t, cfg)
	state := srv.root
	other, row := seedOtherPartition(t, state, 60001, false)
	_, reviewRow := seedOtherPartition(t, state, 60002, true)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	reads := remote.New(client)

	// An approver lists every account.
	rows, err := reads.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending, AllAccounts: true})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	got, err := reads.GetApprovalRequestByPrefix(ctx, row.ID.String()[:8])
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "60001 (other)", got.Requester)

	// A scope above the limit is refused before anything changes.
	_, err = client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow, Scope: "window"})
	var se *ipc.ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeInvalid, se.Code)

	// No login session on either side in a test: the answer is
	// self-elevated, which the configuration allows here.
	res, err := client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow, Scope: "session", Note: "ok"})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, res.State)
	assert.Equal(t, string(approval.AssuranceSelfElevated), res.Assurance)
	assert.NotEmpty(t, res.GrantID)

	after, err := other.GetApprovalRequest(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, after.State)
	assert.Equal(t, "session", after.Scope)
	grant, err := other.MatchApprovalGrant(ctx, row.SessionID, row.ActionDigest, time.Now())
	require.NoError(t, err)
	require.NotNil(t, grant)
	assert.Equal(t, res.GrantID, grant.ID.String())
	receipts, err := other.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: receipt.DecisionApproved})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, res.GrantID, receipts[0].Approval["grant_id"])
	assert.Equal(t, string(approval.AssuranceSelfElevated), receipts[0].Approval["channel"])

	// The first answer wins.
	res, err = client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveDeny})
	require.NoError(t, err)
	assert.Equal(t, "superseded", res.State)
	assert.Contains(t, res.Note, "already approved")
	audits, err := other.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: "approval_superseded"})
	require.NoError(t, err)
	assert.Len(t, audits, 1)

	// A review item records the answer and stores no grant.
	res, err = client.Approve(ctx, ipc.Approve{RequestID: reviewRow.ID, Decision: ipc.ApproveAllow})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, res.State)
	assert.Empty(t, res.GrantID)
}

func TestApproval_SelfElevatedRefusedByDefault(t *testing.T) {
	cfg := approvalConfig()
	cfg.Policy.Approval.LocalAdmin.Group = ownGroup(t)
	srv, sock, _ := startApprovalServer(t, cfg)
	_, row := seedOtherPartition(t, srv.root, 60003, false)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	_, err = client.Approve(context.Background(), ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow})
	var se *ipc.ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeUnauthorized, se.Code)
	assert.Contains(t, se.Message, "self-elevated")
}

func TestApproval_PostHookIsAReviewItem(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	payload := `{"session_id":"test-session-123","cwd":"/home/user/project","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make deploy"},"tool_response":{"stdout":"done"},"tool_use_id":"tu-post"}`
	prompted := false
	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PostToolUse", RawPayload: []byte(payload), InlineWait: 3 * time.Second}, func(_ context.Context, p *ipc.Prompt) ipc.PromptReply {
		prompted = true
		return ipc.PromptReply{Nonce: p.Nonce, Decision: ipc.PromptApprove}
	})
	require.NoError(t, err)
	assert.False(t, prompted, "nobody waits after the action")
	assert.Equal(t, "guidance", string(d.Decision), d.Reason)
	assert.Contains(t, d.Guidance, "review item")
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Review)
	assert.False(t, rows[0].Inline)
}
