package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/internal/version"
	"github.com/spf13/cobra"
)

// newSupervisorProtocolCmd is the hidden test seam of the wire format. It
// runs the connection loop of the decision service over stdin and stdout
// with a handler that decides nothing, so a test can send frames to the
// real decoder and read the real answers without a socket or a service.
func newSupervisorProtocolCmd() *cobra.Command {
	var text bool
	cmd := &cobra.Command{
		Use:    "protocol",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if text {
				out = &frameLines{w: out}
			}
			conn := struct {
				io.Reader
				io.Writer
			}{cmd.InOrStdin(), out}
			welcome := ipc.Welcome{Proto: ipc.Proto, ServerVersion: version.Version, Mode: "none"}
			handler := ipc.HandlerFunc(func(_ context.Context, f *ipc.Frame, _ ipc.Body) (*ipc.Frame, error) {
				return ipc.ErrorFrame(ipc.CodeUnsupported, "no decision service behind this command: "+f.Type), nil
			})
			if err := ipc.ServeConn(context.Background(), conn, welcome, handler); err != nil {
				return WrapError(ExitGeneral, "protocol", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&text, "text", false, "print each reply frame as one JSON line instead of a length-prefixed frame")
	return cmd
}

// frameLines turns the length-prefixed frames written to it into one JSON
// line per frame.
type frameLines struct {
	w   io.Writer
	buf bytes.Buffer
}

func (l *frameLines) Write(p []byte) (int, error) {
	l.buf.Write(p)
	for l.buf.Len() >= 4 {
		n := int(binary.BigEndian.Uint32(l.buf.Bytes()[:4]))
		if l.buf.Len() < 4+n {
			break
		}
		frame := l.buf.Next(4 + n)[4:]
		if _, err := fmt.Fprintf(l.w, "%s\n", frame); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
