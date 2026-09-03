package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// rightStatus renders the header's right-hand side: the tail LIVE/PAUSED
// indicator in tail mode, or a loading spinner and page indicator in
// browse mode, followed by the entry count either way.
func (m appModel) rightStatus() string {
	if m.mode == modeTail {
		status := liveStyle.Render("● LIVE")
		if m.tail.paused {
			status = warningStyle.Render("‖ PAUSED")
		}
		return status + "  " + headerMetaStyle.Render(fmt.Sprintf("%d entries", len(m.tail.entries)))
	}

	loading := ""
	if m.list.loading && len(m.list.entries) > 0 {
		loading = m.spinner.View() + " " + statusStyle.Render("loading more…") + "  "
	}

	count := fmt.Sprintf("%d entries", len(m.list.entries))
	if pages := m.list.pageCount(); pages > 1 {
		if m.list.hasMore {
			// pageCount is only "pages fetched so far" while more exist
			// on the server — there's no fixed total to show yet.
			count = fmt.Sprintf("%s · page %d/%d+", count, m.list.pageNumber(), pages)
		} else {
			count = fmt.Sprintf("%s · page %d/%d", count, m.list.pageNumber(), pages)
		}
	}
	return loading + headerMetaStyle.Render(count)
}

// renderHeader draws tailspin's one-line k9s-style context bar: project,
// active filter, entry count, and (once tail mode lands) a LIVE indicator.
func (m appModel) renderHeader() string {
	left := headerStyle.Render("tailspin") + "  " + headerMetaStyle.Render(m.project)

	middle := headerMetaStyle.Render(m.filter.Build())
	if m.notice != "" {
		middle = errorStyle.Render(m.notice)
	}

	right := m.rightStatus()

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
