package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Terminal styles - matches twdl-go palette.
var (
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

	styleDim = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	styleSuccess = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("2"))

	styleError = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1"))

	styleInput = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

	stylePrompt = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

	styleHighlight = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("3"))
)

// hyperlinkFile wraps path in an OSC 8 terminal hyperlink (file:// URL),
// making it clickable in modern terminals (Windows Terminal, VS Code, iTerm2).
func hyperlinkFile(path string) string {
	slashed := strings.ReplaceAll(path, "\\", "/")
	var url string
	if strings.HasPrefix(slashed, "/") {
		url = "file://" + slashed
	} else {
		url = "file:///" + slashed
	}
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, path)
}

// renderWrap word-wraps s to maxWidth and renders each line with style
// independently so ANSI codes are never split across a physical terminal line.
func renderWrap(style lipgloss.Style, s string, maxWidth int) string {
	if maxWidth <= 0 {
		return style.Render(s)
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return style.Render(s)
	}
	var rendered []string
	line := ""
	for _, w := range words {
		if line == "" {
			line = w
		} else if len([]rune(line))+1+len([]rune(w)) <= maxWidth {
			line += " " + w
		} else {
			rendered = append(rendered, style.Render(line))
			line = w
		}
	}
	if line != "" {
		rendered = append(rendered, style.Render(line))
	}
	return strings.Join(rendered, "\n")
}

// truncate cuts s to maxLen runes, appending "..." if it was shortened.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// hint renders a single keybinding chip: bold-yellow key + dim label.
func hint(key, label string) string {
	return styleHighlight.Render(key) + " " + styleDim.Render(label)
}

// writeErrors appends the error log section to b.
// Shows at most maxErrors entries; older ones are summarised as "...and N more".
func writeErrors(b *strings.Builder, errorLog []logLineMsg, contentWidth, maxErrors int) {
	total := len(errorLog)
	if total == 0 {
		return
	}
	b.WriteString("\n\n")
	shown := errorLog
	overflow := 0
	if total > maxErrors {
		overflow = total - maxErrors
		shown = errorLog[overflow:]
	}
	for _, entry := range shown {
		b.WriteString(renderWrap(styleError, truncate(entry.line, contentWidth), contentWidth))
		b.WriteString("\n")
	}
	if overflow > 0 {
		label := overflowLabel(errorLog[:overflow])
		b.WriteString(renderWrap(styleDim, fmt.Sprintf("...and %d more %s", overflow, label), contentWidth))
		b.WriteString("\n")
	}
}

// writeLog appends the recent activity log to b (dim, one line each).
func writeLog(b *strings.Builder, recentLog []string, contentWidth int) {
	if len(recentLog) == 0 {
		return
	}
	b.WriteString("\n")
	for _, line := range recentLog {
		b.WriteString(renderWrap(styleDim, truncate(line, contentWidth), contentWidth))
		b.WriteString("\n")
	}
}

// overflowLabel returns "warnings", "errors", or "issues" based on the
// severity of the hidden log lines.
func overflowLabel(hidden []logLineMsg) string {
	wCount, eCount := 0, 0
	for _, l := range hidden {
		if l.level >= slog.LevelError {
			eCount++
		} else {
			wCount++
		}
	}
	if wCount > 0 && eCount == 0 {
		return "warnings"
	}
	if eCount > 0 && wCount == 0 {
		return "errors"
	}
	return "issues"
}
