package stats

import (
	"fmt"
	"time"
)

type headerModel struct {
	timeRange   TimeRange
	agentFilter string
	lastRefresh time.Time
	customSince *time.Time
}

func newHeaderModel(timeRange TimeRange, agentFilter string, customSince *time.Time) headerModel {
	return headerModel{
		timeRange:   timeRange,
		agentFilter: agentFilter,
		customSince: customSince,
	}
}

func (h headerModel) view(width int) string {
	title := "gryph stats"
	var rangeTxt string
	if h.customSince != nil {
		rangeTxt = fmt.Sprintf("Since %s", h.customSince.Local().Format("2006-01-02 15:04"))
	} else {
		rangeTxt = h.timeRange.String()
	}

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
