package stats

import "time"

type statsLoadedMsg struct {
	data *StatsData
}

type statsErrorMsg struct {
	err error
}

type tickMsg time.Time
