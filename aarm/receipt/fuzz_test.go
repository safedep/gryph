// Package receipt fuzz harness.
//
// Run the verifier fuzzer with mutation enabled:
//
//	go test -fuzz=FuzzVerifyExportedLog -fuzztime=30s ./aarm/receipt/
//
// The seed corpus runs unconditionally in standard `go test ./aarm/receipt/`.
// Mutation-based fuzzing only runs when the -fuzz flag is set.

package receipt

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// FuzzVerifyExportedLog feeds arbitrary bytes to VerifyExportedLog. The
// property is that the verifier never panics and always returns either a
// structured *LogVerifyResult (possibly with OK=false) or a structured
// error.
func FuzzVerifyExportedLog(f *testing.F) {
	// Seed 1: a clean, single-row JSONL export with a recomputed hash so
	// the chain verifier accepts it.
	f.Add(fuzzCleanExportLine(f))

	// Seed 2: a mildly malformed JSONL stream where the row is missing the
	// hash field. The verifier must report a chain break rather than panic.
	f.Add([]byte(`{"id":"00000000-0000-0000-0000-000000000001","session_id":"00000000-0000-0000-0000-000000000002","sequence":1,"recorded_at":"2026-05-16T12:00:00Z","recorded_at_unix_ns":1747396800000000000,"agent":"claude-code","tool":"Read","action_type":"file_read","decision":"guidance","result_status":"pending","hash":""}
`))

	// Seed 3: an adversarial mix of a long repeated field and an oversized
	// non-base64 signature payload. None of this is a valid receipt. The
	// verifier should surface it as an error or as a non-OK result without
	// panicking.
	long := strings.Repeat("z", 32*1024)
	f.Add([]byte(`{"id":"00000000-0000-0000-0000-000000000001","session_id":"00000000-0000-0000-0000-000000000002","sequence":1,"recorded_at":"2026-05-16T12:00:00Z","recorded_at_unix_ns":1747396800000000000,"agent":"` + long + `","tool":"Read","action_type":"file_read","decision":"guidance","result_status":"pending","hash":"deadbeef","signature":"!!!not-base64!!!","snapshot":{"a":{"b":{"c":{"d":{"e":"x"}}}}}}
`))

	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := VerifyExportedLog(bytes.NewReader(data), nil)
		if err == nil && res == nil {
			t.Fatalf("VerifyExportedLog returned (nil, nil) on len=%d", len(data))
		}
		if err != nil && res != nil {
			t.Fatalf("VerifyExportedLog returned both a result and an error on len=%d", len(data))
		}
		if err == nil && res.Total > 0 && !res.OK {
			// Any non-OK result for malformed input must surface at least
			// one structured break or signature error.
			if res.ChainBreaks == 0 && res.SignedInvalid == 0 && len(res.Errors) == 0 {
				t.Fatalf("non-OK result with no diagnostics on len=%d res=%+v", len(data), res)
			}
		}
	})
}

// fuzzCleanExportLine builds a single-row JSONL export whose hash matches
// the canonical recompute so the verifier accepts it cleanly.
func fuzzCleanExportLine(f *testing.F) []byte {
	f.Helper()
	sessionID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	actionID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	eventID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	receiptID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	recorded := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	fields := HashInputFields{
		Sequence:       1,
		PrevHash:       nil,
		RecordedAtUnix: recorded.UnixNano(),
		SessionID:      sessionID,
		ActionID:       actionID,
		EventID:        eventID,
		Agent:          "claude-code",
		Tool:           "Read",
		ActionType:     "file_read",
		Project:        "",
		Decision:       "guidance",
		Severity:       "medium",
		Message:        "msg",
		MatchedRuleIDs: []string{"r-1"},
		Snapshot:       map[string]interface{}{"total_actions": 1},
		ActionPayload:  map[string]interface{}{"path": "/tmp/x"},
	}
	hash, err := ComputeHash(NewHashInput(fields))
	if err != nil {
		f.Fatalf("seed hash compute: %v", err)
	}

	line := fmt.Sprintf(
		`{"id":"%s","session_id":"%s","action_id":"%s","event_id":"%s","sequence":1,"recorded_at":"%s","recorded_at_unix_ns":%d,"agent":"claude-code","tool":"Read","action_type":"file_read","decision":"guidance","matched_rule_ids":["r-1"],"severity":"medium","message":"msg","result_status":"pending","snapshot":{"total_actions":1},"action_payload":{"path":"/tmp/x"},"hash":"%s"}`+"\n",
		receiptID, sessionID, actionID, eventID,
		recorded.Format(time.RFC3339Nano), recorded.UnixNano(),
		hex.EncodeToString(hash),
	)
	return []byte(line)
}
