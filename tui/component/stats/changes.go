package stats

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/safedep/gryph/tui"
)

func renderChanges(data *StatsData, width, height int) string {
	var b strings.Builder

	net := data.LinesAdded - data.LinesRemoved
	b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n",
		greenValueStyle.Render(fmt.Sprintf("+%s", tui.FormatNumber(data.LinesAdded))),
		redValueStyle.Render(fmt.Sprintf("-%s", tui.FormatNumber(data.LinesRemoved))),
		valueStyle.Render(fmt.Sprintf("net %+d", net)),
		labelStyle.Render(fmt.Sprintf("(%d files)", data.UniqueFilesModified)),
	))

	if len(data.TopFiles) == 0 {
		return renderPanel("CODE CHANGES", b.String(), width, height)
	}

	prefix := commonPathPrefix(data.WorkingDirs, data.TopFiles)

	// Available rows: height minus title(1), summary(1), blank separator(1)
	maxFiles := height - 3
	if maxFiles < 1 {
		maxFiles = 1
	}
	if maxFiles > len(data.TopFiles) {
		maxFiles = len(data.TopFiles)
	}

	// Find max total changes for proportional bars
	maxChanges := 0
	for _, f := range data.TopFiles[:maxFiles] {
		total := f.LinesAdded + f.LinesRemoved
		if total > maxChanges {
			maxChanges = total
		}
	}

	// Layout: "  path  bar  +N -M  (Nx)"
	barWidth := 10
	statsWidth := 22 // " +NNN -NNN  (Nx)"
	pathWidth := width - barWidth - statsWidth - 6
	if pathWidth < 12 {
		pathWidth = 12
	}

	for i := 0; i < maxFiles; i++ {
		f := data.TopFiles[i]

		path := shortenPath(f.Path, prefix, pathWidth)

		bar := ""
		if maxChanges > 0 {
			added := (f.LinesAdded * barWidth) / maxChanges
			removed := (f.LinesRemoved * barWidth) / maxChanges
			if added+removed > barWidth {
				removed = barWidth - added
			}
			remaining := barWidth - added - removed
			bar = greenValueStyle.Render(strings.Repeat("█", added)) +
				redValueStyle.Render(strings.Repeat("█", removed)) +
				labelStyle.Render(strings.Repeat("░", remaining))
		}

		changes := fmt.Sprintf("%s %s",
			greenValueStyle.Render(fmt.Sprintf("+%d", f.LinesAdded)),
			redValueStyle.Render(fmt.Sprintf("-%d", f.LinesRemoved)),
		)

		edits := ""
		if f.WriteCount > 1 {
			edits = labelStyle.Render(fmt.Sprintf(" %dx", f.WriteCount))
		}

		b.WriteString(fmt.Sprintf("  %s %s %s%s\n",
			labelStyle.Width(pathWidth).Render(path),
			bar,
			changes,
			edits,
		))
	}

	return renderPanel("CODE CHANGES", b.String(), width, height)
}

func commonPathPrefix(workingDirs []string, files []FileStat) string {
	// If we have a single working directory, use it
	if len(workingDirs) == 1 {
		return workingDirs[0]
	}

	// Otherwise compute longest common prefix from file paths
	if len(files) == 0 {
		return ""
	}

	prefix := filepath.Dir(files[0].Path)
	for _, f := range files[1:] {
		dir := filepath.Dir(f.Path)
		for !strings.HasPrefix(dir, prefix) {
			parent := filepath.Dir(prefix)
			if parent == prefix {
				return ""
			}
			prefix = parent
		}
	}
	return prefix
}

func shortenPath(path, prefix string, maxWidth int) string {
	rel := path
	if prefix != "" {
		if r, err := filepath.Rel(prefix, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}

	if len(rel) <= maxWidth {
		return rel
	}

	// Try showing dir/file
	dir := filepath.Dir(rel)
	base := filepath.Base(rel)
	short := filepath.Base(dir) + "/" + base
	if len(short) <= maxWidth {
		return short
	}

	return tui.TruncateString(base, maxWidth)
}
