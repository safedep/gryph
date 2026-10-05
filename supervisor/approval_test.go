//go:build linux || darwin

package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const escalatePolicy = `version: "1"
rules:
  - id: escalate-npm
    action: escalate
    min_assurance: same-user-tty
    match:
      action_types: [command_exec]
      command_patterns: ["npm install"]
    message: "npm install needs approval"
  - id: escalate-deploy
    action: escalate
    match:
      action_types: [command_exec]
      command_patterns: ["make deploy"]
`

func commandPayload(command, toolUse string) string {
	return `{"session_id":"test-session-123","cwd":"/home/user/project","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"` + command + `"},"tool_use_id":"` + toolUse + `"}`
}

// startApprovalServer starts a server whose partition of this uid carries
// the escalate policy, with the approval keys of cfg.
func startApprovalServer(t *testing.T, cfg *config.Config) (*Server, string, *storage.SQLiteStore) {
	t.Helper()
	cfg.Policy.Enabled = true
	cfg.Policy.LogAllEvaluations = true
	// The host that runs the tests may run polkit. The approval tests
	// stay hermetic with no authority.
	srv, state, _ := startServerWithOptions(t, cfg, Options{Authorizer: &fakeAuthority{}})
	dir := filepath.Join(state, "users", strconv.Itoa(os.Getuid()))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(escalatePolicy), 0o600))
	sock := filepath.Join(filepath.Dir(state), "hook.sock")
	store, err := storage.NewSQLiteStore(filepath.Join(dir, "audit.db"))
	require.NoError(t, err)
	require.NoError(t, store.Init(context.Background()))
	t.Cleanup(func() { _ = store.Close() })
	return srv, sock, store
}

func approvalConfig() *config.Config {
	cfg := config.Default()
	cfg.Policy.Approval.Channels = []string{config.ApprovalChannelSameUserTTY, config.ApprovalChannelLocalAdmin}
	cfg.Policy.Approval.MinAssurance = config.ApprovalChannelLocalAdmin
	cfg.Policy.Approval.InlineWait = 5 * time.Second
	cfg.Policy.Approval.RequestTTL = time.Hour
	return cfg
}

func answer(decision, note string) ipc.PromptFunc {
	return func(_ context.Context, p *ipc.Prompt) ipc.PromptReply {
		return ipc.PromptReply{Nonce: p.Nonce, Decision: decision, Note: note}
	}
}

func TestApproval_InlineApproveAppliesOnceAndStoresNoGrant(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	var prompts []*ipc.Prompt
	handle := ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("npm install", "tu-1")), InlineWait: 3 * time.Second}
	d, err := client.Handle(ctx, handle, func(_ context.Context, p *ipc.Prompt) ipc.PromptReply {
		prompts = append(prompts, p)
		return ipc.PromptReply{Nonce: p.Nonce, Decision: ipc.PromptApprove, Note: "release window"}
	})
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision), d.Reason)
	require.Len(t, prompts, 1)
	assert.Equal(t, "npm install", prompts[0].View.Command)
	assert.Equal(t, []string{"escalate-npm"}, prompts[0].View.Rules)
	assert.NotEmpty(t, prompts[0].RequestID)
	assert.WithinDuration(t, time.Now().Add(3*time.Second), prompts[0].Deadline, time.Second, "the deadline is the client's wait, under the configured one")

	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, storage.ApprovalRequestApproved, rows[0].State)
	assert.Equal(t, config.ApprovalChannelSameUserTTY, rows[0].Channel)
	assert.Equal(t, string(approval.AssuranceSameUserTTY), rows[0].Assurance)
	assert.Equal(t, "release window", rows[0].Note)
	assert.True(t, rows[0].Inline)
	assert.NotNil(t, rows[0].NotifiedAt, "the hook that asked carries the outcome")
	assert.Equal(t, "command_exec: npm install", rows[0].Summary)

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: receipt.DecisionApproved})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, config.ApprovalChannelSameUserTTY, receipts[0].Approval["channel"])
	assert.Equal(t, approval.PeerTrustUnknown, receipts[0].Approval["peer_trust"])
	assert.Equal(t, rows[0].ID.String(), receipts[0].Approval["request_id"])
	assert.Contains(t, receipts[0].Approval["approver"], "on the terminal")

	// The same action asks again: the terminal stores no grant.
	handle.RawPayload = []byte(commandPayload("npm install", "tu-2"))
	d, err = client.Handle(ctx, handle, answer(ipc.PromptDeny, "not now"))
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	assert.Contains(t, d.Reason, "Denied by")
	assert.Contains(t, d.Reason, "not now")
	rows, err = store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestDenied})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	grant, err := store.MatchApprovalGrant(ctx, rows[0].SessionID, rows[0].ActionDigest, time.Now())
	require.NoError(t, err)
	assert.Nil(t, grant)
}

func TestApproval_NoAnswerLeavesTheRequestPending(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	handle := ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("npm install", "tu-1")), InlineWait: 3 * time.Second}
	d, err := client.Handle(ctx, handle, nil)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	assert.Contains(t, d.Reason, "This action needs approval. Request ")
	assert.Contains(t, d.Reason, "Do not retry until the user confirms that it is approved. Check status: gryph policy approve show ")

	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Contains(t, d.Reason, rows[0].ID.String()[:8])
	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: "escalate"})
	require.NoError(t, err)
	require.Len(t, receipts, 1, "the receipt keeps escalate while the request is open")
	assert.Equal(t, rows[0].ID.String(), receipts[0].Approval["request_id"])

	// A reply outside a prompt never counts, whatever its nonce.
	conn := dial(t, sock)
	hello(t, conn)
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypePromptReply, ipc.PromptReply{Nonce: "any", Decision: ipc.PromptApprove}))
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), "no prompt is open")
	rows, err = store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestApproval_NoWaitAndHighFloorSkipThePrompt(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	prompted := 0
	count := func(_ context.Context, p *ipc.Prompt) ipc.PromptReply {
		prompted++
		return ipc.PromptReply{Nonce: p.Nonce, Decision: ipc.PromptApprove}
	}

	// A client that cannot wait gets the pending block at once.
	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("npm install", "tu-1"))}, count)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	assert.Contains(t, d.Reason, "needs approval")
	assert.Equal(t, 0, prompted)

	// A rule above the terminal's assurance never prompts the terminal.
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-2")), InlineWait: 3 * time.Second}, count)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	assert.Equal(t, 0, prompted)
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.False(t, r.Inline)
	}

	// With no channel that meets the floor the note says so.
	cfg := approvalConfig()
	cfg.Policy.Approval.Channels = []string{config.ApprovalChannelSameUserTTY}
	_, sock2, _ := startApprovalServer(t, cfg)
	client2, err := ipc.Dial(context.Background(), sock2, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client2.Close() }()
	d, err = client2.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-3")), InlineWait: 3 * time.Second}, count)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	assert.Contains(t, d.Reason, "No approval channel meets min_assurance local-admin.")
	assert.Equal(t, 0, prompted)
}

func TestApproval_ExpiryDeniesAndTheNextHookReportsIt(t *testing.T) {
	cfg := approvalConfig()
	cfg.Policy.Approval.RequestTTL = 300 * time.Millisecond
	_, sock, store := startApprovalServer(t, cfg)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("npm install", "tu-1"))}, nil)
	require.NoError(t, err)
	require.Equal(t, "block", string(d.Decision))
	time.Sleep(400 * time.Millisecond)

	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}, nil)
	require.NoError(t, err)
	assert.Equal(t, "guidance", string(d.Decision))
	assert.Contains(t, d.Guidance, "expired without an answer")

	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, storage.ApprovalRequestExpired, rows[0].State)
	assert.Equal(t, expiryApprover, rows[0].Approver)
	assert.NotNil(t, rows[0].NotifiedAt)
	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: receipt.DecisionApprovalTimeout})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, "blocked", receipts[0].ResultStatus)
	assert.Equal(t, expiryApprover, receipts[0].Approval["approver"])

	// Reported once.
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}, nil)
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision))
}

func TestApproval_GrantApprovesTheRetry(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
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
	req := rows[0]

	// A stronger channel answers later and stores a grant for the digest.
	require.NoError(t, store.ResolveApprovalRequest(ctx, req.ID, storage.ApprovalResolution{State: storage.ApprovalRequestApproved, Channel: config.ApprovalChannelLocalAdmin, Assurance: string(approval.AssuranceLocalAdmin), Approver: "admin", Scope: "once"}))
	require.NoError(t, store.InsertApprovalGrant(ctx, &storage.ApprovalGrantRow{RequestID: req.ID, SessionID: req.SessionID, ActionDigest: req.ActionDigest, Scope: "once", ExpiresAt: time.Now().Add(time.Hour), Approver: "admin", Assurance: string(approval.AssuranceLocalAdmin), Channel: config.ApprovalChannelLocalAdmin}))

	// The next hook reports the approval, and the retry uses the grant.
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}, nil)
	require.NoError(t, err)
	assert.Contains(t, d.Guidance, "was approved. You can retry.")

	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-2"))}, nil)
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision), d.Reason)
	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: receipt.DecisionApproved})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.NotEmpty(t, receipts[0].Approval["grant_id"])
	assert.Equal(t, config.ApprovalChannelLocalAdmin, receipts[0].Approval["channel"])

	// Once is once: the second retry asks again.
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-3"))}, nil)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))

	// Another directory is another action.
	other := strings.Replace(commandPayload("make deploy", "tu-4"), "/home/user/project", "/home/user/other", 1)
	require.NoError(t, store.InsertApprovalGrant(ctx, &storage.ApprovalGrantRow{RequestID: req.ID, SessionID: req.SessionID, ActionDigest: req.ActionDigest, Scope: "session", ExpiresAt: time.Now().Add(time.Hour), Approver: "admin"}))
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(other)}, nil)
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))
	d, err = client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("make deploy", "tu-5"))}, nil)
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision))
}

func TestApproval_WrongNonceCountsAsNoAnswer(t *testing.T) {
	_, sock, store := startApprovalServer(t, approvalConfig())
	conn := dial(t, sock)
	hello(t, conn)
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(commandPayload("npm install", "tu-1")), InlineWait: 3 * time.Second}))
	require.Equal(t, ipc.TypePrompt, reply.Type, string(reply.Body))
	reply = exchange(t, conn, ipc.MustFrame(ipc.TypePromptReply, ipc.PromptReply{Nonce: "forged", Decision: ipc.PromptApprove}))
	require.Equal(t, ipc.TypeDecision, reply.Type, string(reply.Body))
	assert.Contains(t, string(reply.Body), `"decision":"block"`)
	rows, err := store.QueryApprovalRequests(context.Background(), &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}
