package listen

import (
	"errors"
	"net"
	"syscall"

	"github.com/safedep/gryph/platform/internal/macos"
	"github.com/safedep/gryph/platform/launchd"
)

// activated asks launchd for the socket of the job. launchd opens the
// socket as root before the job starts and keeps it open across a restart
// of the job. A process that launchd did not start (ESRCH), or a job
// without the socket (ENOENT), opens its own.
func activated() ([]net.Listener, error) {
	fds, err := macos.ActivateSocket(launchd.SocketName)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return listeners(fds)
}
