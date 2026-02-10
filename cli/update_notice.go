package cli

import (
	"github.com/safedep/gryph/internal/selfupdate"
	"github.com/safedep/gryph/tui"
)

func renderUpdateNotice(presenter tui.Presenter, ch <-chan *selfupdate.UpdateResult) {
	select {
	case result, ok := <-ch:
		if ok && result != nil && result.UpdateAvailable {
			_ = presenter.RenderUpdateNotice(&tui.UpdateNoticeView{
				CurrentVersion: result.CurrentVersion,
				LatestVersion:  result.LatestVersion,
				ReleaseURL:     result.ReleaseURL,
			})
		}
	default:
	}
}
