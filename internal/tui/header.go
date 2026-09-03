package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderHeader draws tailspin's one-line k9s-style context bar: project,
// active filter, entry count, and (once tail mode lands) a LIVE indicator.
func (m appModel) renderHeader() string {
	left := headerStyle.Render("tailspin") + "  " + headerMetaStyle.Render(m.project)

	middle := headerMetaStyle.Render(m.filter.Build())
	if m.notice != "" {
		middle = errorStyle.Render(m.notice)
	}

	right := headerMetaStyle.Render(fmt.Sprintf("%d entries", len(m.list.entries)))
	if m.mode == modeTail {
		right = liveStyle.Render("● LIVE") + "  " + right
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", middle)
	return padBetween(bar, right, m.width)
}

// padBetween places left flush-left and right flush-right within width,
// falling back to a simple concatenation if there isn't room.
func padBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
