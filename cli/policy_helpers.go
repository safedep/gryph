package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/storage"
)

// detectOperatorIdentity returns the operator's login name from the
// conventional environment variables, falling back to "operator" when none
// are set. Thin shim over approval.DefaultOperatorIdentity so the CLI
// deferral commands and the AARM CLIPrompt approver default share a single
// source of truth without a circular import.
func detectOperatorIdentity() string {
	return approval.DefaultOperatorIdentity()
}

// resolveAarmSessionID resolves a session reference (full UUID, context-state
// prefix, or session-row prefix) to the canonical session UUID. Shared by the
// AARM-facing CLI commands (policy receipts, policy context) so the lookup
// order and error message stay in lockstep.
func resolveAarmSessionID(ctx context.Context, store storage.Store, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	if state, err := store.GetContextStateByPrefix(ctx, ref); err == nil && state != nil {
		return state.SessionID, nil
	}
	sess, err := store.GetSessionByPrefix(ctx, ref)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve session reference: %w", err)
	}
	if sess != nil {
		return sess.ID, nil
	}
	return uuid.Nil, fmt.Errorf("no session or context state matches reference %q", ref)
}
