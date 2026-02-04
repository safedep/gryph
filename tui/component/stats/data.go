package stats

import (
	"context"
	"sort"
	"time"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
)

type AgentStat struct {
	Name         string
	Sessions     int
	Events       int
	FilesRead    int
	FilesWritten int
	Commands     int
	Errors       int
}

type FileStat struct {
	Path         string
	WriteCount   int
	LinesAdded   int
	LinesRemoved int
}

type CommandStat struct {
	Command   string
	Count     int
	FailCount int
}

type StatsData struct {
	TotalEvents    int
	TotalSessions  int
	ActiveSessions int
	UniqueAgents   int

	FileReads       int
	FileWrites      int
	FileDeletes     int
	CommandExecs    int
	ToolUses        int
	NetworkRequests int

	Agents []AgentStat

	HourlyBuckets [24]int

	LinesAdded        int
	LinesRemoved      int
	UniqueFilesModified int
	TopFiles          []FileStat
	WorkingDirs       []string

	TotalCommands  int
	PassedCommands int
	FailedCommands int
	TopCommands    []CommandStat

	TotalErrors    int
	TotalBlocked   int
	TotalRejected  int

	AvgDuration       time.Duration
	AvgActionsPerSess float64
	LongestSession    time.Duration
	ShortestSession   time.Duration

	TimeSpanStart time.Time
	TimeSpanEnd   time.Time
}

func computeStats(ctx context.Context, store storage.Store, since *time.Time, agentFilter string) (*StatsData, error) {
	data := &StatsData{}

	sessionFilter := session.NewSessionFilter().WithLimit(10000)
	if since != nil {
		sessionFilter = sessionFilter.WithSince(*since)
	}
	if agentFilter != "" {
		sessionFilter = sessionFilter.WithAgent(agentFilter)
	}

	sessions, err := store.QuerySessions(ctx, sessionFilter)
	if err != nil {
		return nil, err
	}

	data.TotalSessions = len(sessions)
	agentMap := map[string]*AgentStat{}
	workingDirSet := map[string]bool{}

	var totalDuration time.Duration
	var sessionCount int

	for _, s := range sessions {
		if s.IsActive() {
			data.ActiveSessions++
		}

		if s.WorkingDirectory != "" {
			workingDirSet[s.WorkingDirectory] = true
		}

		as, ok := agentMap[s.AgentName]
		if !ok {
			as = &AgentStat{Name: s.AgentName}
			agentMap[s.AgentName] = as
		}
		as.Sessions++
		as.Events += s.TotalActions
		as.FilesRead += s.FilesRead
		as.FilesWritten += s.FilesWritten
		as.Commands += s.CommandsExecuted
		as.Errors += s.Errors

		dur := s.Duration()
		if dur > 0 {
			totalDuration += dur
			sessionCount++
			if dur > data.LongestSession {
				data.LongestSession = dur
			}
			if data.ShortestSession == 0 || dur < data.ShortestSession {
				data.ShortestSession = dur
			}
		}
	}

	if sessionCount > 0 {
		data.AvgDuration = totalDuration / time.Duration(sessionCount)
	}

	data.UniqueAgents = len(agentMap)
	for _, as := range agentMap {
		data.Agents = append(data.Agents, *as)
	}
	sort.Slice(data.Agents, func(i, j int) bool {
		return data.Agents[i].Events > data.Agents[j].Events
	})

	eventFilter := events.NewEventFilter().WithLimit(0)
	if since != nil {
		eventFilter = eventFilter.WithSince(*since)
	}
	if agentFilter != "" {
		eventFilter = eventFilter.WithAgents(agentFilter)
	}

	evts, err := store.QueryEvents(ctx, eventFilter)
	if err != nil {
		return nil, err
	}

	data.TotalEvents = len(evts)

	fileStats := map[string]*FileStat{}
	cmdStats := map[string]*CommandStat{}

	var totalActions int
	for _, s := range sessions {
		totalActions += s.TotalActions
	}
	if data.TotalSessions > 0 {
		data.AvgActionsPerSess = float64(totalActions) / float64(data.TotalSessions)
	}

	for _, e := range evts {
		if data.TimeSpanStart.IsZero() || e.Timestamp.Before(data.TimeSpanStart) {
			data.TimeSpanStart = e.Timestamp
		}
		if e.Timestamp.After(data.TimeSpanEnd) {
			data.TimeSpanEnd = e.Timestamp
		}

		hour := e.Timestamp.Local().Hour()
		data.HourlyBuckets[hour]++

		switch e.ResultStatus {
		case events.ResultError:
			data.TotalErrors++
		case events.ResultBlocked:
			data.TotalBlocked++
		case events.ResultRejected:
			data.TotalRejected++
		}

		switch e.ActionType {
		case events.ActionFileRead:
			data.FileReads++
		case events.ActionFileWrite:
			data.FileWrites++
			if p, err := e.GetFileWritePayload(); err == nil && p != nil {
				data.LinesAdded += p.LinesAdded
				data.LinesRemoved += p.LinesRemoved
				fs, ok := fileStats[p.Path]
				if !ok {
					fs = &FileStat{Path: p.Path}
					fileStats[p.Path] = fs
				}
				fs.WriteCount++
				fs.LinesAdded += p.LinesAdded
				fs.LinesRemoved += p.LinesRemoved
			}
		case events.ActionFileDelete:
			data.FileDeletes++
		case events.ActionCommandExec:
			data.CommandExecs++
			data.TotalCommands++
			if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
				if p.ExitCode == 0 {
					data.PassedCommands++
				} else {
					data.FailedCommands++
				}
				cs, ok := cmdStats[p.Command]
				if !ok {
					cs = &CommandStat{Command: p.Command}
					cmdStats[p.Command] = cs
				}
				cs.Count++
				if p.ExitCode != 0 {
					cs.FailCount++
				}
			}
		case events.ActionToolUse:
			data.ToolUses++
		case events.ActionNetworkRequest:
			data.NetworkRequests++
		}
	}

	data.UniqueFilesModified = len(fileStats)
	for dir := range workingDirSet {
		data.WorkingDirs = append(data.WorkingDirs, dir)
	}
	sort.Strings(data.WorkingDirs)

	for _, fs := range fileStats {
		data.TopFiles = append(data.TopFiles, *fs)
	}
	sort.Slice(data.TopFiles, func(i, j int) bool {
		return data.TopFiles[i].WriteCount > data.TopFiles[j].WriteCount
	})
	if len(data.TopFiles) > 10 {
		data.TopFiles = data.TopFiles[:10]
	}

	for _, cs := range cmdStats {
		data.TopCommands = append(data.TopCommands, *cs)
	}
	sort.Slice(data.TopCommands, func(i, j int) bool {
		return data.TopCommands[i].Count > data.TopCommands[j].Count
	})
	if len(data.TopCommands) > 10 {
		data.TopCommands = data.TopCommands[:10]
	}

	return data, nil
}
