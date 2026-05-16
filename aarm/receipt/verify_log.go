package receipt

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/google/uuid"
)

// LogVerifyResult is the aggregate verdict of VerifyExportedLog. Failures are
// reported per-receipt in Errors. SignedUnverified counts rows that carry a
// signature but the caller did not provide a trust store. Such rows are not
// failures: they surface as warnings so an operator can still inspect a chain
// without staging keys.
type LogVerifyResult struct {
	Total            int            `json:"total"`
	SignedOK         int            `json:"signed_ok"`
	Unsigned         int            `json:"unsigned"`
	SignedInvalid    int            `json:"signed_invalid"`
	SignedUnverified int            `json:"signed_unverified"`
	ChainBreaks      int            `json:"chain_breaks"`
	Errors           []LogVerifyErr `json:"errors,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
	OK               bool           `json:"ok"`
	Sessions         map[string]int `json:"sessions,omitempty"`
}

// LogVerifyErr is a single per-receipt failure surfaced by VerifyExportedLog.
type LogVerifyErr struct {
	SessionID string `json:"session_id,omitempty"`
	Sequence  int64  `json:"sequence"`
	ReceiptID string `json:"receipt_id,omitempty"`
	Reason    string `json:"reason"`
}

// VerifyExportedLog reads a JSONL stream of ExportedReceipt rows from r,
// re-derives each row's hash, validates the per-session hash chain, and
// optionally checks the signature against verifier (when non-nil and a
// signature is present). Errors are accumulated per-row so callers see all
// failures from a single pass.
func VerifyExportedLog(r io.Reader, verifier Verifier) (*LogVerifyResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var rows []ExportedReceipt
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec ExportedReceipt
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("receipt: parse jsonl line: %w", err)
		}
		rows = append(rows, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("receipt: read jsonl: %w", err)
	}

	bySession := map[string][]ExportedReceipt{}
	for _, r := range rows {
		bySession[r.SessionID] = append(bySession[r.SessionID], r)
	}

	res := &LogVerifyResult{
		Total:    len(rows),
		Sessions: map[string]int{},
	}
	for sid, group := range bySession {
		sort.Slice(group, func(i, j int) bool { return group[i].Sequence < group[j].Sequence })
		res.Sessions[sid] = len(group)
		verifyOneExportedChain(group, verifier, res)
	}
	if res.SignedUnverified > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("%d signed receipt(s) reported as unverified: no trust store was provided", res.SignedUnverified))
	}
	res.OK = res.ChainBreaks == 0 && res.SignedInvalid == 0
	return res, nil
}

// chainRowFromExported decodes the hex hash fields and parses session/action/
// event UUIDs out of an ExportedReceipt so it can feed VerifyChain.
func chainRowFromExported(row ExportedReceipt) (ChainRow, error) {
	hashBytes, err := hex.DecodeString(row.Hash)
	if err != nil {
		return ChainRow{}, fmt.Errorf("decode hash: %w", err)
	}
	prevHashBytes, err := hex.DecodeString(row.PrevHash)
	if err != nil {
		return ChainRow{}, fmt.Errorf("decode prev_hash: %w", err)
	}
	sessID, err := uuid.Parse(row.SessionID)
	if err != nil {
		return ChainRow{}, fmt.Errorf("parse session_id: %w", err)
	}
	var actionID, eventID uuid.UUID
	if row.ActionID != "" {
		actionID, err = uuid.Parse(row.ActionID)
		if err != nil {
			return ChainRow{}, fmt.Errorf("parse action_id: %w", err)
		}
	}
	if row.EventID != "" {
		eventID, err = uuid.Parse(row.EventID)
		if err != nil {
			return ChainRow{}, fmt.Errorf("parse event_id: %w", err)
		}
	}
	var policyHashBytes []byte
	if row.PolicyHash != "" {
		policyHashBytes, err = hex.DecodeString(row.PolicyHash)
		if err != nil {
			return ChainRow{}, fmt.Errorf("decode policy_hash: %w", err)
		}
	}
	return ChainRow{
		SessionID: sessID,
		Sequence:  row.Sequence,
		PrevHash:  prevHashBytes,
		Hash:      hashBytes,
		Fields: HashInputFields{
			Sequence:       row.Sequence,
			PrevHash:       prevHashBytes,
			RecordedAtUnix: row.RecordedAtUnix,
			SessionID:      sessID,
			ActionID:       actionID,
			EventID:        eventID,
			Agent:          row.Agent,
			Tool:           row.Tool,
			ActionType:     row.ActionType,
			Project:        row.Project,
			Decision:       row.Decision,
			Severity:       row.Severity,
			Message:        row.Message,
			MatchedRuleIDs: row.MatchedRuleIDs,
			Snapshot:       row.Snapshot,
			ActionPayload:  row.ActionPayload,
			SubagentID:     row.SubagentID,
			SubagentType:   row.SubagentType,
			PolicyHash:     policyHashBytes,
		},
	}, nil
}

func verifyOneExportedChain(rows []ExportedReceipt, verifier Verifier, out *LogVerifyResult) {
	type pair struct {
		row     ExportedReceipt
		chainOK bool
		cr      ChainRow
	}
	pairs := make([]pair, 0, len(rows))
	for _, row := range rows {
		cr, err := chainRowFromExported(row)
		if err != nil {
			out.ChainBreaks++
			out.Errors = append(out.Errors, LogVerifyErr{
				SessionID: row.SessionID,
				Sequence:  row.Sequence,
				ReceiptID: row.ID,
				Reason:    err.Error(),
			})
			pairs = append(pairs, pair{row: row})
			continue
		}
		pairs = append(pairs, pair{row: row, chainOK: true, cr: cr})
	}

	chainRows := make([]ChainRow, 0, len(pairs))
	for _, p := range pairs {
		if p.chainOK {
			chainRows = append(chainRows, p.cr)
		}
	}
	for _, b := range VerifyChain(chainRows) {
		out.ChainBreaks++
		var (
			sid string
			rid string
		)
		for _, p := range pairs {
			if p.chainOK && p.cr.Sequence == b.Sequence && p.cr.SessionID == b.SessionID {
				sid = p.row.SessionID
				rid = p.row.ID
				break
			}
		}
		out.Errors = append(out.Errors, LogVerifyErr{
			SessionID: sid,
			Sequence:  b.Sequence,
			ReceiptID: rid,
			Reason:    b.Reason,
		})
	}

	for _, p := range pairs {
		if !p.chainOK {
			continue
		}
		row := p.row
		if row.Signature == "" {
			out.Unsigned++
			continue
		}
		sigBytes, derr := base64.StdEncoding.DecodeString(row.Signature)
		switch {
		case derr != nil:
			out.SignedInvalid++
			out.Errors = append(out.Errors, LogVerifyErr{
				SessionID: row.SessionID,
				Sequence:  row.Sequence,
				ReceiptID: row.ID,
				Reason:    fmt.Sprintf("decode signature: %v", derr),
			})
		case verifier == nil:
			out.SignedUnverified++
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("session=%s seq=%d signature present but no trust store provided", row.SessionID, row.Sequence))
		default:
			if err := verifier.Verify(p.cr.Hash, sigBytes, row.SignerKeyID); err != nil {
				out.SignedInvalid++
				out.Errors = append(out.Errors, LogVerifyErr{
					SessionID: row.SessionID,
					Sequence:  row.Sequence,
					ReceiptID: row.ID,
					Reason:    err.Error(),
				})
			} else {
				out.SignedOK++
			}
		}
	}
}
