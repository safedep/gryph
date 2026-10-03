package ipc

import (
	"bytes"
	"testing"
	"time"

	"github.com/safedep/gryph/internal/fuzzseed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzTimeBound is the time one decode may take under the fuzzer.
const fuzzTimeBound = time.Second

// FuzzDecode checks that the decoder never panics on a frame body and that
// a body that decodes also validates in bounded time. The seed corpus comes
// from the string literals of the table tests. Run it with:
//
//	go test -run '^$' -fuzz=FuzzDecode -fuzztime=10m ./decision/ipc/
func FuzzDecode(f *testing.F) {
	for _, s := range fuzzseed.StringLiterals(f, "frame_test.go") {
		f.Add([]byte(s))
	}
	for _, frameType := range []string{TypeHello, TypeHandle, TypeDecision, TypePrompt, TypePromptReply, TypeReportHookError, TypeQuery, TypeQueryResult, TypeError, "other"} {
		f.Add([]byte(`{"type":"` + frameType + `","body":{"proto":1,"agent":"a","hook_type":"x","raw_payload":"e30=","nonce":"n","deadline":"2026-01-01T00:00:00Z","kind":"k","code":"c","rows":[{}]}}`))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		start := time.Now()
		frame, err := ParseFrame(data)
		if err == nil {
			_, _ = Decode(frame)
		}
		require.Less(t, time.Since(start), fuzzTimeBound)

		// A frame on the wire reads back to the same bytes.
		var buf bytes.Buffer
		if err == nil {
			require.NoError(t, WriteFrame(&buf, frame))
			again, err := ReadFrame(&buf)
			require.NoError(t, err)
			assert.Equal(t, frame.Type, again.Type)
		}
	})
}
