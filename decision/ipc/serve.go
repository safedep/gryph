package ipc

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Handler answers the frames of one connection after the handshake. A
// handler returns the frame to send back. It never sees a frame that failed
// its bounds or a type this binary does not know: the loop answers those
// with Error itself.
type Handler interface {
	Serve(ctx context.Context, f *Frame, body Body) (*Frame, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, f *Frame, body Body) (*Frame, error)

// Serve implements Handler.
func (fn HandlerFunc) Serve(ctx context.Context, f *Frame, body Body) (*Frame, error) {
	return fn(ctx, f, body)
}

// ServeConn runs one connection: it waits for Hello, answers with welcome,
// then answers every frame through the handler until the peer closes or a
// read fails. A frame the loop cannot decode gets Error and the loop goes
// on, so a bad frame from a peer never ends the connection of a good one
// that follows. A frame type the loop does not know gets CodeUnsupported.
func ServeConn(ctx context.Context, rw io.ReadWriter, welcome Welcome, handler Handler) error {
	first, err := ReadFrame(rw)
	if err != nil {
		return err
	}
	if first.Type != TypeHello {
		_ = WriteFrame(rw, ErrorFrame(CodeInvalid, "the first frame must be hello"))
		return fmt.Errorf("%w: first frame %q", ErrInvalid, first.Type)
	}
	if _, err := Decode(first); err != nil {
		_ = WriteFrame(rw, ErrorFrame(CodeInvalid, err.Error()))
		return err
	}
	if err := WriteFrame(rw, MustFrame(TypeWelcome, welcome)); err != nil {
		return err
	}
	for {
		f, err := ReadFrame(rw)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, ErrFrameTooLarge), errors.Is(err, ErrEmptyFrame):
			// The stream is out of step after a refused length, so the
			// connection ends with one error frame.
			_ = WriteFrame(rw, ErrorFrame(CodeInvalid, err.Error()))
			return err
		case err != nil:
			return err
		}
		reply := answer(ctx, f, handler)
		if err := WriteFrame(rw, reply); err != nil {
			return err
		}
	}
}

func answer(ctx context.Context, f *Frame, handler Handler) *Frame {
	body, err := Decode(f)
	switch {
	case errors.Is(err, ErrUnsupported):
		return ErrorFrame(CodeUnsupported, err.Error())
	case err != nil:
		return ErrorFrame(CodeInvalid, err.Error())
	}
	reply, err := handler.Serve(ctx, f, body)
	if err != nil {
		return ErrorFrame(CodeInternal, err.Error())
	}
	if reply == nil {
		return MustFrame(TypeAck, Ack{})
	}
	return reply
}

// ErrorFrame builds an Error frame.
func ErrorFrame(code, message string) *Frame {
	if len(message) > MaxText {
		message = message[:MaxText]
	}
	return MustFrame(TypeError, Error{Code: code, Message: message})
}
