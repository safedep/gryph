package livelog

import (
	"fmt"
	"time"
)

type headerModel struct {
	agentFilter string
	sessionCount int
}

func newHeaderModel(agentFilter string) headerModel {
	return headerModel{agentFilter: agentFilter}
}

func (h headerModel) view(width int) string {
	title := "gryph live"

	agent := "all"
	if h.agentFilter != "" {
		agent = h.agentFilter
	}

	clock := time.Now().Format("15:04:05")
	sessions := fmt.Sprintf("%d sessions", h.sessionCount)

	content := fmt.Sprintf(" %s | %s | %s | %s", title, agent, sessions, clock)
	return headerStyle.Width(width).Render(content)
}
