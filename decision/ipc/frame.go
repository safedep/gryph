// Package ipc is the wire format between the hook client, which runs as the
// agent user, and the decision service, which runs outside the user. A frame
// is a 4-byte big-endian length and a JSON document. The server treats every
// field as an untrusted claim and bounds every string, so a flood of bytes
// from a same-user process costs it a bounded read and nothing else.
package ipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Proto is the protocol version this binary speaks. A peer with another
// version still exchanges Hello and Welcome, and each side treats a frame
// type it does not know as unsupported.
const Proto = 1

// MaxFrame bounds one frame on the wire. A raw agent payload can hold a
// tool output of a few megabytes, and 8 MiB leaves room for it and the
// envelope. A larger frame is refused before any byte of it is decoded.
const MaxFrame = 8 << 20

// The frame types. A type the receiver does not know gets Error with
// CodeUnsupported.
const (
	TypeHello           = "hello"
	TypeWelcome         = "welcome"
	TypeHandle          = "handle"
	TypeDecision        = "decision"
	TypePrompt          = "prompt"
	TypePromptReply     = "prompt_reply"
	TypeReportHookError = "report_hook_error"
	TypeAck             = "ack"
	TypeQuery           = "query"
	TypeQueryResult     = "query_result"
	TypeSessionCost     = "session_cost"
	TypeImportEvents    = "import_events"
	TypeImportReceipts  = "import_receipts"
	TypeImportSession   = "import_session"
	TypeImportResult    = "import_result"
	TypeApprove         = "approve"
	TypeApproveResult   = "approve_result"
	TypeError           = "error"
)

// The error codes an Error frame carries. A client maps every one of them
// to the verdict its fail mode names.
const (
	CodeUnsupported  = "unsupported"
	CodeInvalid      = "invalid"
	CodeRateLimited  = "rate_limited"
	CodeDeadline     = "deadline"
	CodeUnauthorized = "unauthorized"
	CodeInternal     = "internal"
)

var (
	// ErrFrameTooLarge says the length prefix names more than MaxFrame.
	ErrFrameTooLarge = errors.New("ipc: frame larger than the limit")
	// ErrEmptyFrame says the length prefix names zero bytes.
	ErrEmptyFrame = errors.New("ipc: empty frame")
	// ErrUnsupported says the frame type is not one this binary knows.
	ErrUnsupported = errors.New("ipc: unsupported frame type")
	// ErrInvalid says a frame body failed its bounds.
	ErrInvalid = errors.New("ipc: invalid frame")
)

// Frame is one message on the wire: its type and its body as JSON. Unknown
// fields in the envelope and in the body are ignored, so a newer peer can
// add fields.
type Frame struct {
	Type string          `json:"type"`
	Body json.RawMessage `json:"body,omitempty"`
}

const headerLen = 4

// ReadFrame reads one frame. It reads the length first and refuses a
// length above MaxFrame before it reads the body.
func ReadFrame(r io.Reader) (*Frame, error) {
	var header [headerLen]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	switch {
	case n == 0:
		return nil, ErrEmptyFrame
	case n > MaxFrame:
		return nil, ErrFrameTooLarge
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return ParseFrame(data)
}

// ParseFrame decodes the envelope of one frame body.
func ParseFrame(data []byte) (*Frame, error) {
	if len(data) > MaxFrame {
		return nil, ErrFrameTooLarge
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if f.Type == "" {
		return nil, fmt.Errorf("%w: no type", ErrInvalid)
	}
	return &f, nil
}

// WriteFrame writes one frame with its length prefix.
func WriteFrame(w io.Writer, f *Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(data) > MaxFrame {
		return ErrFrameTooLarge
	}
	var header [headerLen]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// NewFrame builds a frame of the type with body encoded as JSON.
func NewFrame(frameType string, body any) (*Frame, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &Frame{Type: frameType, Body: data}, nil
}

// MustFrame is NewFrame for a body that always encodes.
func MustFrame(frameType string, body any) *Frame {
	f, err := NewFrame(frameType, body)
	if err != nil {
		panic(err)
	}
	return f
}
