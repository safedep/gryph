package stats

import (
	"fmt"
	"time"
)

type headerModel struct {
	timeRange   TimeRange
	agentFilter string
	lastRefresh time.Time
}

func newHeaderModel(timeRange TimeRange, agentFilter string) headerModel {
	return headerModel{
		timeRange:   timeRange,
		agentFilter: agentFilter,
	}
}

func (h headerModel) view(width int) string {
	title := "gryph stats"
	rangeTxt := h.timeRange.String()

	agent := "all agents"
	if h.agentFilter != "" {
		agent = h.agentFilter
	}

	refresh := ""
	if !h.lastRefresh.IsZero() {
		refresh = fmt.Sprintf("Refreshed %s", h.lastRefresh.Local().Format("15:04"))
	}

	content := fmt.Sprintf(" %s │ %s │ %s │ %s", title, rangeTxt, agent, refresh)
	return titleStyle.Width(width).Render(content)
}
