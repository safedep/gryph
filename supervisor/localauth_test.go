//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/platform/localauth"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuthority stands in for polkit.
type fakeAuthority struct {
	available bool
	result    localauth.Result
	err       error
	subjects  []localauth.Subject
}

func (f *fakeAuthority) Available(context.Context) bool { return f.available }

func (f *fakeAuthority) Authorize(_ context.Context, s localauth.Subject, action string, interactive bool) (localauth.Result, error) {
	f.subjects = append(f.subjects, s)
	if action != localauth.ActionID || !interactive {
		return localauth.Result{}, errors.New("unexpected call")
	}
	return f.result, f.err
}

func startAuthServer(t *testing.T, auth localauth.Authorizer, allowWithout bool) (*Server, string) {
	t.Helper()
	cfg := approvalConfig()
	cfg.Policy.Approval.LocalAdmin.Group = ownGroup(t)
	cfg.Policy.Approval.LocalAdmin.AllowSelfElevated = true
	cfg.Policy.Approval.LocalAdmin.AllowWithoutAuth = allowWithout
	cfg.Policy.Enabled = true
	cfg.Policy.LogAllEvaluations = true
	srv, state, _ := startServerWithOptions(t, cfg, Options{Authorizer: auth})
	return srv, filepath.Join(filepath.Dir(state), "hook.sock")
}

func TestLocalAuth_PasswordRequiredWhenTheHostHasAnAuthority(t *testing.T) {
	auth := &fakeAuthority{available: true, result: localauth.Result{Challenge: true}}
	srv, sock := startAuthServer(t, auth, false)
	other, row := seedOtherPartition(t, srv.root, 60011, false)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	_, err = client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow})
	var se *ipc.ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ipc.CodeUnauthorized, se.Code)
	assert.Contains(t, se.Message, "needs the password of the approver")
	assert.Contains(t, se.Message, "no password was given")
	require.Len(t, auth.subjects, 1)
	assert.NotZero(t, auth.subjects[0].PID)
	assert.NotZero(t, auth.subjects[0].StartTime)
	after, err := other.GetApprovalRequest(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestPending, after.State)

	// A dismissed prompt is a refusal too.
	auth.result = localauth.Result{Challenge: true, Dismissed: true}
	_, err = client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow})
	require.ErrorAs(t, err, &se)
	assert.Contains(t, se.Message, "dismissed")

	// With the password the answer is local-auth, also from a process
	// with no login session: the password proves the person.
	auth.result = localauth.Result{Authorized: true}
	res, err := client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveDeny})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestDenied, res.State)
	assert.Equal(t, string(approval.AssuranceLocalAuth), res.Assurance)
}

func TestLocalAuth_AllowWithoutAuthTakesTheAnswer(t *testing.T) {
	auth := &fakeAuthority{available: true, result: localauth.Result{Challenge: true}}
	srv, sock := startAuthServer(t, auth, true)
	other, row := seedOtherPartition(t, srv.root, 60012, false)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	res, err := client.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, res.State)
	assert.Equal(t, string(approval.AssuranceSelfElevated), res.Assurance)
	receipts, err := other.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: receipt.DecisionApproved})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
}

func TestLocalAuth_NoAuthorityTakesTheAnswer(t *testing.T) {
	auth := &fakeAuthority{available: false}
	srv, sock := startAuthServer(t, auth, false)
	_, row := seedOtherPartition(t, srv.root, 60013, false)
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	res, err := client.Approve(context.Background(), ipc.Approve{RequestID: row.ID, Decision: ipc.ApproveAllow})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, res.State)
	assert.Empty(t, auth.subjects, "a host without an authority asks nobody")
}

// With the password given, the answer is local-auth, and a request from
// another login session is no different.
func TestLocalAuth_AuthorizedLocalAdminBecomesLocalAuth(t *testing.T) {
	auth := &fakeAuthority{available: true, result: localauth.Result{Authorized: true}}
	srv, sock := startAuthServer(t, auth, false)
	other, row := seedOtherPartition(t, srv.root, 60014, false)
	ctx := context.Background()
	// A requester with a login session of its own. The approver of the
	// test has none, so the pair differs and the answer is local-admin
	// before the password.
	rowWithAudit := *row
	rowWithAudit.ID = [16]byte{9}
	rowWithAudit.ReceiptSequence = 2
	rowWithAudit.RequesterAudit = "1000"
	require.NoError(t, other.InsertApprovalRequest(ctx, &rowWithAudit))
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	res, err := client.Approve(ctx, ipc.Approve{RequestID: rowWithAudit.ID, Decision: ipc.ApproveAllow})
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, res.State)
	assert.Equal(t, string(approval.AssuranceLocalAuth), res.Assurance)
	_ = config.ApprovalChannelLocalAuth
}
