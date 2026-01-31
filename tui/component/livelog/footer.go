package livelog

import "fmt"

type footerModel struct {
	paused     bool
	scrollLock bool
	lastError  string
}

func newFooterModel() footerModel {
	return footerModel{}
}

func (f footerModel) view(width int) string {
	hints := " q quit  p pause  ? help  a agent  1-5 filter  G bottom"

	var indicators string
	if f.paused {
		indicators += "  " + pauseIndicatorStyle.Render("PAUSED")
	}
	if f.scrollLock {
		indicators += "  " + scrollLockStyle.Render("SCROLL")
	}
	if f.lastError != "" {
		indicators += fmt.Sprintf("  err: %s", f.lastError)
	}

	return footerStyle.Width(width).Render(hints + indicators)
}
