package cli

import (
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/internal/selfupdate"
	"github.com/safedep/gryph/tui"
)

// renderUpdateNotice renders an update notice if one is available on the channel.
// This must be non-blocking so that CLI commands are not delayed by the update check.
// The select default case ensures we skip silently if the async check hasn't completed.
func renderUpdateNotice(presenter tui.Presenter, ch <-chan *selfupdate.UpdateResult) {
	select {
	case result, ok := <-ch:
		if ok && result != nil && result.UpdateAvailable {
			if err := presenter.RenderUpdateNotice(&tui.UpdateNoticeView{
				CurrentVersion: result.CurrentVersion,
				LatestVersion:  result.LatestVersion,
				ReleaseURL:     result.ReleaseURL,
			}); err != nil {
				log.Errorf("failed to render update notice: %v", err)
			}
		}
	default:
	}
}
