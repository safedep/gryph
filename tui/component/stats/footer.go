package stats

type footerModel struct {
	lastError string
}

func newFooterModel() footerModel {
	return footerModel{}
}

func (f footerModel) view(width int) string {
	hints := " q quit  ? help  t today  w week  m month  a all  r refresh"
	if f.lastError != "" {
		hints += "  err: " + f.lastError
	}
	return footerStyle.Width(width).Render(hints)
}
