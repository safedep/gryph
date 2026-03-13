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
	customUntil *time.Time
}

func newHeaderModel(timeRange TimeRange, agentFilter string, customSince, customUntil *time.Time) headerModel {
	return headerModel{
		timeRange:   timeRange,
		agentFilter: agentFilter,
		customSince: customSince,
		customUntil: customUntil,
	}
}

func (h headerModel) view(width int, activeTab tab) string {
	var rangeTxt string
	if h.customSince != nil && h.customUntil != nil {
		rangeTxt = fmt.Sprintf("%s – %s",
			h.customSince.Local().Format("Jan 2 15:04"),
			h.customUntil.Local().Format("Jan 2 15:04"))
	} else if h.customSince != nil {
		rangeTxt = fmt.Sprintf("Since %s", h.customSince.Local().Format("2006-01-02 15:04"))
	} else if h.customUntil != nil {
		rangeTxt = fmt.Sprintf("%s until %s", h.timeRange.String(), h.customUntil.Local().Format("Jan 2 15:04"))
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

	tabs := renderTabs(activeTab)
	content := fmt.Sprintf(" %s │ %s │ %s │ %s", tabs, rangeTxt, agent, refresh)
	return titleStyle.Width(width).Render(content)
}

func renderTabs(active tab) string {
	tabs := []struct {
		label string
		t     tab
	}{
		{"Overview", tabOverview},
		{"Cost", tabCost},
	}

	var parts []string
	for _, t := range tabs {
		if t.t == active {
			parts = append(parts, fmt.Sprintf("[%s]", t.label))
		} else {
			parts = append(parts, fmt.Sprintf(" %s ", t.label))
		}
	}
	return fmt.Sprintf("%s %s", parts[0], parts[1])
}
