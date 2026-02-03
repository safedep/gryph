package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderPanel(title string, content string, width, height int) string {
	titleLine := panelTitleStyle.Render(title)
	body := titleLine + "\n" + content

	return panelStyle.
		Width(width - 2). // account for border
		Height(height).
		Render(body)
}

func renderBar(filled, total, width int) string {
	if total == 0 || width <= 0 {
		return ""
	}
	filledW := (filled * width) / total
	if filledW > width {
		filledW = width
	}
	emptyW := width - filledW
	return strings.Repeat("█", filledW) + strings.Repeat("░", emptyW)
}

func renderSparkline(values []int, width int) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}

	blocks := []rune(" ▁▂▃▄▅▆▇█")
	subsPerRow := len(blocks) - 1 // 8 sub-levels per row

	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return strings.Repeat(string(blocks[0]), width)
	}

	// Single-row fallback
	return renderSparklineMultiRow(values, width, 1, blocks, subsPerRow, max)
}

func renderSparklineMultiRow(values []int, width, rows int, blocks []rune, subsPerRow, max int) string {
	totalSubs := rows * subsPerRow

	// Map each output column to a bucket value scaled to totalSubs
	cols := make([]int, width)
	for i := 0; i < width; i++ {
		bucket := (i * len(values)) / width
		if bucket >= len(values) {
			bucket = len(values) - 1
		}
		cols[i] = (values[bucket] * totalSubs) / max
	}

	var out strings.Builder
	for row := rows - 1; row >= 0; row-- {
		threshold := row * subsPerRow
		for col := 0; col < width; col++ {
			fill := cols[col] - threshold
			if fill <= 0 {
				out.WriteRune(blocks[0]) // space
			} else if fill >= subsPerRow {
				out.WriteRune(blocks[subsPerRow]) // full block
			} else {
				out.WriteRune(blocks[fill])
			}
		}
		if row > 0 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func renderVerticalChart(values []int, width, rows int) string {
	if len(values) == 0 || width <= 0 || rows <= 0 {
		return ""
	}

	blocks := []rune(" ▁▂▃▄▅▆▇█")
	subsPerRow := len(blocks) - 1

	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		var out strings.Builder
		for r := 0; r < rows; r++ {
			out.WriteString(strings.Repeat(" ", width))
			if r < rows-1 {
				out.WriteByte('\n')
			}
		}
		return out.String()
	}

	return renderSparklineMultiRow(values, width, rows, blocks, subsPerRow, max)
}

func percentage(part, total int) string {
	if total == 0 {
		return "0%"
	}
	pct := float64(part) * 100 / float64(total)
	if pct < 1 && part > 0 {
		return fmt.Sprintf("%.1f%%", pct)
	}
	return fmt.Sprintf("%.0f%%", pct)
}

func twoColumnGrid(left, right string, width int) string {
	half := width / 2
	leftStyled := lipgloss.NewStyle().Width(half).Render(left)
	rightStyled := lipgloss.NewStyle().Width(width - half).Render(right)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, rightStyled)
}

func singleColumnStack(panels ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}
