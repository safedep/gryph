package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrame_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := MustFrame(TypeHandle, Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(`{"tool_name":"Bash"}`), Project: decision.ProjectClaim{Name: "p"}})
	require.NoError(t, WriteFrame(&buf, in))
	assert.Equal(t, uint32(buf.Len()-4), binary.BigEndian.Uint32(buf.Bytes()[:4]), "the prefix is the body length")

	out, err := ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, TypeHandle, out.Type)
	body, err := Decode(out)
	require.NoError(t, err)
	h, ok := body.(*Handle)
	require.True(t, ok)
	assert.Equal(t, "claude-code", h.Agent)
	assert.JSONEq(t, `{"tool_name":"Bash"}`, string(h.RawPayload))
}

func TestReadFrame_Limits(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		err  error
	}{
		{name: "empty", data: []byte{0, 0, 0, 0}, err: ErrEmptyFrame},
		{name: "too large", data: []byte{0, 0x80, 0, 1}, err: ErrFrameTooLarge},
		{name: "short body", data: []byte{0, 0, 0, 9, '{', '}'}, err: io.ErrUnexpectedEOF},
		{name: "no header", data: []byte{0, 0}, err: io.ErrUnexpectedEOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(tc.data))
			assert.ErrorIs(t, err, tc.err)
		})
	}
}

func TestDecode(t *testing.T) {
	long := strings.Repeat("x", MaxName+1)
	cases := []struct {
		name  string
		frame *Frame
		err   error
	}{
		{name: "hello", frame: MustFrame(TypeHello, Hello{Proto: 1, ClientVersion: "1.0"})},
		{name: "hello without proto", frame: MustFrame(TypeHello, Hello{}), err: ErrInvalid},
		{name: "hello too many caps", frame: MustFrame(TypeHello, Hello{Proto: 1, Caps: make([]string, MaxCaps+1)}), err: ErrInvalid},
		{name: "welcome", frame: MustFrame(TypeWelcome, Welcome{Proto: 1, ServerVersion: "1.0", Mode: "enforce"})},
		{name: "handle long agent", frame: MustFrame(TypeHandle, Handle{Agent: long, HookType: "x", RawPayload: []byte("{}")}), err: ErrInvalid},
		{name: "handle no agent", frame: MustFrame(TypeHandle, Handle{HookType: "x"}), err: ErrInvalid},
		{name: "handle payload too large", frame: MustFrame(TypeHandle, Handle{Agent: "a", HookType: "x", RawPayload: bytes.Repeat([]byte("a"), MaxRawPayload+1)}), err: ErrInvalid},
		{name: "decision unknown verdict decodes", frame: MustFrame(TypeDecision, Decision{Decision: "defer"})},
		{name: "decision long reason", frame: MustFrame(TypeDecision, Decision{Decision: "block", Reason: strings.Repeat("r", MaxText+1)}), err: ErrInvalid},
		{name: "prompt", frame: MustFrame(TypePrompt, Prompt{Nonce: "n", ActionDigest: "d", Deadline: time.Now()})},
		{name: "prompt no deadline", frame: MustFrame(TypePrompt, Prompt{Nonce: "n"}), err: ErrInvalid},
		{name: "prompt reply", frame: MustFrame(TypePromptReply, PromptReply{Nonce: "n", Decision: PromptApprove})},
		{name: "prompt reply bad decision", frame: MustFrame(TypePromptReply, PromptReply{Nonce: "n", Decision: "allow"}), err: ErrInvalid},
		{name: "prompt reply no nonce", frame: MustFrame(TypePromptReply, PromptReply{Decision: PromptApprove}), err: ErrInvalid},
		{name: "report hook error", frame: MustFrame(TypeReportHookError, decision.HookError{Agent: "a", HookType: "x", RawSize: 3, Message: "m"})},
		{name: "report hook error big raw", frame: MustFrame(TypeReportHookError, decision.HookError{Agent: "a", RawEvent: bytes.Repeat([]byte("a"), MaxRawEvent+1)}), err: ErrInvalid},
		{name: "ack", frame: MustFrame(TypeAck, Ack{})},
		{name: "query", frame: MustFrame(TypeQuery, Query{Kind: "sessions", Params: map[string]string{"agent": "a"}, Limit: 10})},
		{name: "query no kind", frame: MustFrame(TypeQuery, Query{}), err: ErrInvalid},
		{name: "query result", frame: MustFrame(TypeQueryResult, QueryResult{Rows: []json.RawMessage{json.RawMessage(`{}`)}})},
		{name: "error", frame: MustFrame(TypeError, Error{Code: CodeRateLimited})},
		{name: "error no code", frame: MustFrame(TypeError, Error{}), err: ErrInvalid},
		{name: "unknown type", frame: &Frame{Type: "shutdown"}, err: ErrUnsupported},
		{name: "unknown field ignored", frame: &Frame{Type: TypeHello, Body: json.RawMessage(`{"proto":1,"client_version":"1","future":true}`)}},
		{name: "body not an object", frame: &Frame{Type: TypeHello, Body: json.RawMessage(`[1]`)}, err: ErrInvalid},
		{name: "invalid utf8", frame: &Frame{Type: TypeError, Body: json.RawMessage(`{"code":"\xff"}`)}, err: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := Decode(tc.frame)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, body)
		})
	}
}

func TestParseFrame_NoType(t *testing.T) {
	_, err := ParseFrame([]byte(`{"body":{}}`))
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = ParseFrame([]byte(`not json`))
	assert.ErrorIs(t, err, ErrInvalid)
}

// pipeConn joins a reader and a writer into one io.ReadWriter.
type pipeConn struct {
	io.Reader
	io.Writer
}

func TestServeConn(t *testing.T) {
	var in, out bytes.Buffer
	require.NoError(t, WriteFrame(&in, MustFrame(TypeHello, Hello{Proto: Proto, ClientVersion: "t"})))
	require.NoError(t, WriteFrame(&in, &Frame{Type: "shutdown"}))
	require.NoError(t, WriteFrame(&in, MustFrame(TypeQuery, Query{})))
	require.NoError(t, WriteFrame(&in, MustFrame(TypeHandle, Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")})))
	require.NoError(t, WriteFrame(&in, MustFrame(TypeReportHookError, decision.HookError{Agent: "a"})))

	handler := HandlerFunc(func(_ context.Context, f *Frame, body Body) (*Frame, error) {
		if f.Type == TypeHandle {
			return MustFrame(TypeDecision, Decision{Decision: "allow"}), nil
		}
		return nil, nil
	})
	err := ServeConn(context.Background(), pipeConn{&in, &out}, Welcome{Proto: Proto, ServerVersion: "t", Mode: "enforce"}, handler)
	require.NoError(t, err)

	var replies []*Frame
	for {
		f, err := ReadFrame(&out)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		replies = append(replies, f)
	}
	require.Len(t, replies, 5)
	assert.Equal(t, TypeWelcome, replies[0].Type)
	assert.Equal(t, TypeError, replies[1].Type)
	assert.Contains(t, string(replies[1].Body), CodeUnsupported)
	assert.Equal(t, TypeError, replies[2].Type)
	assert.Contains(t, string(replies[2].Body), CodeInvalid)
	assert.Equal(t, TypeDecision, replies[3].Type)
	assert.Equal(t, TypeAck, replies[4].Type, "a frame with no answer gets ack")
}

func TestServeConn_FirstFrameMustBeHello(t *testing.T) {
	var in, out bytes.Buffer
	require.NoError(t, WriteFrame(&in, MustFrame(TypeQuery, Query{Kind: "x"})))
	err := ServeConn(context.Background(), pipeConn{&in, &out}, Welcome{Proto: Proto}, HandlerFunc(func(context.Context, *Frame, Body) (*Frame, error) { return nil, nil }))
	assert.ErrorIs(t, err, ErrInvalid)
	f, err := ReadFrame(&out)
	require.NoError(t, err)
	assert.Equal(t, TypeError, f.Type)
}

func TestServeConn_TooLargeEndsConnection(t *testing.T) {
	var in, out bytes.Buffer
	require.NoError(t, WriteFrame(&in, MustFrame(TypeHello, Hello{Proto: Proto})))
	in.Write([]byte{0xff, 0, 0, 0})
	err := ServeConn(context.Background(), pipeConn{&in, &out}, Welcome{Proto: Proto}, HandlerFunc(func(context.Context, *Frame, Body) (*Frame, error) { return nil, nil }))
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}
