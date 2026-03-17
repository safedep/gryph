package query

import "github.com/safedep/gryph/core/events"

type sessionSummary struct {
	filesWritten   []fileSummary
	filesRead      int
	filesDeleted   int
	commands       []cmdSummary
	commandsFailed int
	sensitive      int
	blocked        int
	errors         int
}

type fileSummary struct {
	path         string
	linesAdded   int
	linesRemoved int
}

type cmdSummary struct {
	command  string
	exitCode int
}

func computeSummary(evts []*events.Event) sessionSummary {
	var s sessionSummary
	for _, e := range evts {
		switch e.ActionType {
		case events.ActionFileRead:
			s.filesRead++
		case events.ActionFileWrite:
			fs := fileSummary{}
			if p, err := e.GetFileWritePayload(); err == nil && p != nil {
				fs.path = p.Path
				fs.linesAdded = p.LinesAdded
				fs.linesRemoved = p.LinesRemoved
			}
			s.filesWritten = append(s.filesWritten, fs)
		case events.ActionFileDelete:
			s.filesDeleted++
		case events.ActionCommandExec:
			cs := cmdSummary{}
			if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
				cs.command = p.Command
				cs.exitCode = p.ExitCode
				if p.ExitCode != 0 {
					s.commandsFailed++
				}
			}
			s.commands = append(s.commands, cs)
		}

		if e.IsSensitive {
			s.sensitive++
		}
		if e.ResultStatus == events.ResultBlocked || e.ResultStatus == events.ResultRejected {
			s.blocked++
		}
		if e.ResultStatus == events.ResultError {
			s.errors++
		}
	}
	return s
}

