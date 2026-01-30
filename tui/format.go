package tui

import (
	"fmt"
	"strings"
	"time"
)

// FormatBytes formats bytes as a human-readable string.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats a duration as a human-readable string.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// FormatTime formats a time for display.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

// FormatTimeShort formats a time with just hour:minute:second.
func FormatTimeShort(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("15:04:05")
}

// FormatDate formats a date for display.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// FormatNumber formats a number with thousand separators.
func FormatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatNumberWithSep(n)
}

func formatNumberWithSep(n int) string {
	s := fmt.Sprintf("%d", n)
	var result strings.Builder
	l := len(s)
	for i, c := range s {
		if i > 0 && (l-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// FormatShortID returns the first 8 characters of an ID.
func FormatShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// FormatLineChanges formats line changes as "+N -M".
func FormatLineChanges(added, removed int) string {
	if added == 0 && removed == 0 {
		return ""
	}
	if removed == 0 {
		return fmt.Sprintf("+%d", added)
	}
	if added == 0 {
		return fmt.Sprintf("-%d", removed)
	}
	return fmt.Sprintf("+%d -%d", added, removed)
}

// FormatExitCode formats an exit code.
func FormatExitCode(code int) string {
	return fmt.Sprintf("exit:%d", code)
}

// TruncateString truncates a string to the given length.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// PadRight pads a string to the right to achieve the given width.
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// PadLeft pads a string to the left to achieve the given width.
func PadLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// HorizontalLine returns a horizontal line of the given width.
func HorizontalLine(width int) string {
	return strings.Repeat("─", width)
}

// TreePrefix returns the tree prefix for a list item.
func TreePrefix(isLast bool) string {
	if isLast {
		return "└─ "
	}
	return "├─ "
}

// TreeContinue returns the tree continuation prefix.
func TreeContinue(hasMore bool) string {
	if hasMore {
		return "│  "
	}
	return "   "
}
