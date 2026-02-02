package livelog

import (
	"fmt"
	"strings"
	"time"

	"github.com/safedep/gryph/core/events"
)

const sidebarWidth = 22

type statsModel struct {
	totalEvents int
	byAction    map[events.ActionType]int
	byStatus    map[events.ResultStatus]int
	agents      map[string]bool
	recentTimes []time.Time
}

func newStatsModel() statsModel {
	return statsModel{
		byAction: make(map[events.ActionType]int),
		byStatus: make(map[events.ResultStatus]int),
		agents:   make(map[string]bool),
	}
}

func (s *statsModel) record(e *events.Event) {
	s.totalEvents++
	s.byAction[e.ActionType]++
	s.byStatus[e.ResultStatus]++
	s.agents[e.AgentName] = true
	s.recentTimes = append(s.recentTimes, e.Timestamp)
	if len(s.recentTimes) > 120 {
		s.recentTimes = s.recentTimes[1:]
	}
}

func (s *statsModel) eventsPerMinute() int {
	if len(s.recentTimes) < 2 {
		return 0
	}
	first := s.recentTimes[0]
	last := s.recentTimes[len(s.recentTimes)-1]
	dur := last.Sub(first)
	if dur < time.Second {
		return 0
	}
	return int(float64(len(s.recentTimes)) / dur.Minutes())
}

func (s statsModel) view(height int) string {
	var b strings.Builder

	b.WriteString(sidebarHeaderStyle.Render("Stats"))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf(" Events: %s\n", sidebarValueStyle.Render(fmt.Sprintf("%d", s.totalEvents))))
	b.WriteString(fmt.Sprintf(" Rate:   %s\n", sidebarValueStyle.Render(fmt.Sprintf("%d/min", s.eventsPerMinute()))))
	b.WriteByte('\n')

	b.WriteString(sidebarHeaderStyle.Render("By Type"))
	b.WriteByte('\n')
	actionOrder := []events.ActionType{
		events.ActionFileRead, events.ActionFileWrite, events.ActionCommandExec,
		events.ActionToolUse, events.ActionNetworkRequest,
	}
	for _, a := range actionOrder {
		if count, ok := s.byAction[a]; ok && count > 0 {
			as := actionStyleFor(a)
			label := fmt.Sprintf(" %s %-8s", as.symbol, a.DisplayName())
			b.WriteString(fmt.Sprintf("%s %s\n",
				sidebarLabelStyle.Render(label),
				sidebarValueStyle.Render(fmt.Sprintf("%d", count))))
		}
	}
	b.WriteByte('\n')

	b.WriteString(sidebarHeaderStyle.Render("By Status"))
	b.WriteByte('\n')
	for _, st := range []events.ResultStatus{events.ResultSuccess, events.ResultError, events.ResultBlocked} {
		if count, ok := s.byStatus[st]; ok && count > 0 {
			styled := statusStyleFor(st).Render(fmt.Sprintf("%-8s", string(st)))
			b.WriteString(fmt.Sprintf(" %s %s\n", styled, sidebarValueStyle.Render(fmt.Sprintf("%d", count))))
		}
	}
	b.WriteByte('\n')

	b.WriteString(sidebarHeaderStyle.Render("Sessions"))
	b.WriteByte('\n')
	for name := range s.agents {
		b.WriteString(fmt.Sprintf(" %s\n", agentBadge(name)))
	}

	return sidebarStyle.Width(sidebarWidth).Height(height).Render(b.String())
}
