package cli

import (
	"context"
	"net"
	"os"

	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/platform/listen"
	"github.com/spf13/cobra"
)

// newSupervisorFakeCmd is the hidden test seam of the hook client: a
// service that misbehaves on purpose. With --no-welcome it holds every
// connection without a word, as a service that hangs at start. With
// --reply it answers every frame after the handshake with the frame in
// the file, as a newer service that speaks a verdict this binary does not
// know. Without either it answers ack.
func newSupervisorFakeCmd() *cobra.Command {
	var (
		socket    string
		noWelcome bool
		replyFile string
	)
	cmd := &cobra.Command{
		Use:    "fake",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if socket == "" {
				return ErrConfig("invalid flags", errNoSocket)
			}
			var reply *ipc.Frame
			if replyFile != "" {
				data, err := os.ReadFile(replyFile)
				if err != nil {
					return WrapError(ExitGeneral, "read the reply", err)
				}
				reply, err = ipc.ParseFrame(data)
				if err != nil {
					return WrapError(ExitGeneral, "parse the reply", err)
				}
			}
			ln, err := listen.Open(socket)
			if err != nil {
				return WrapError(ExitGeneral, "open the socket", err)
			}
			defer func() { _ = ln.Close() }()
			for {
				conn, err := ln.Accept()
				if err != nil {
					return nil
				}
				go serveFake(conn, noWelcome, reply)
			}
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "the socket to listen at")
	cmd.Flags().BoolVar(&noWelcome, "no-welcome", false, "hold every connection and never answer")
	cmd.Flags().StringVar(&replyFile, "reply", "", "a frame file to answer every request with")
	return cmd
}

func serveFake(conn net.Conn, noWelcome bool, reply *ipc.Frame) {
	defer func() { _ = conn.Close() }()
	if noWelcome {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}
	welcome := ipc.Welcome{Proto: ipc.Proto, ServerVersion: version.Version + "-fake", Mode: "enforce"}
	_ = ipc.ServeConn(context.Background(), conn, welcome, ipc.HandlerFunc(func(context.Context, *ipc.Frame, ipc.Body) (*ipc.Frame, error) {
		return reply, nil
	}))
}
