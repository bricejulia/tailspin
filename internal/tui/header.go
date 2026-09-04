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

// rightStatusPlain is rightStatus's content with no styling at all — used
// as a safely-truncatable fallback by renderHeaderTop when the styled
// version doesn't fit (see there for why byte-slicing the styled version
// instead isn't safe). The spinner glyph is dropped here: it's the most
// purely decorative part, and the one this fallback can afford to lose
// first if space is genuinely this tight.
func (m appModel) rightStatusPlain() string {
	if m.mode == modeTail {
		status := "● LIVE"
		if m.tail.paused {
			status = "‖ PAUSED"
		}
		return fmt.Sprintf("%s  %d entries", status, len(m.tail.entries))
	}

	loading := ""
	if m.list.loading && len(m.list.entries) > 0 {
		loading = "loading more…  "
	}

	count := fmt.Sprintf("%d entries", len(m.list.entries))
	if pages := m.list.pageCount(); pages > 1 {
		if m.list.hasMore {
			count = fmt.Sprintf("%s · page %d/%d+", count, m.list.pageNumber(), pages)
		} else {
			count = fmt.Sprintf("%s · page %d/%d", count, m.list.pageNumber(), pages)
		}
	}
	return loading + count
}

// renderHeader draws tailspin's two-line k9s-style context bar: a top line
// with the project and status (entry/page count, LIVE/loading indicator),
// and a second line dedicated to the active query — its own full-width
// line, not squeezed into the middle of the top one, so a real query is
// actually readable instead of getting truncated almost immediately (see
// headerLines, the matching budget contentHeight reserves for it).
func (m appModel) renderHeader() string {
	return m.renderHeaderTop() + "\n" + m.renderHeaderQueryLine()
}

// headerLines is how many physical lines renderHeader always renders,
// regardless of content — contentHeight's budget must match this exactly.
const headerLines = 2

func (m appModel) renderHeaderTop() string {
	left := headerStyle.Render("tailspin") + "  " + headerMetaStyle.Render(m.project)
	right := m.rightStatus()
	if lipgloss.Width(left)+lipgloss.Width(right) < m.width {
		return padBetween(left, right, m.width)
	}

	// Not enough room for both, styled, as they are. Two things this
	// must not do: silently drop the right side (the entry/page status —
	// the half worth keeping visible, per feedback that it was
	// disappearing) the way padBetween's plain fallback does when it's
	// handed something that already doesn't fit, and byte-slice an
	// already-ANSI-styled string to shrink it (risks cutting an escape
	// sequence in half and corrupting everything rendered after it).
	// Falling back to plain, safely-truncatable text for both sides
	// avoids the latter; shrinking the project name first, and only the
	// status if even an empty project name wouldn't help, gets the former.
	rightPlain := m.rightStatusPlain()
	if w := lipgloss.Width(rightPlain); w > m.width {
		rightPlain = truncate(rightPlain, m.width)
	}
	leftPlain := ""
	if avail := m.width - lipgloss.Width(rightPlain) - 1; avail > 0 {
		leftPlain = truncate("tailspin  "+m.project, avail)
	}
	gap := max(m.width-lipgloss.Width(leftPlain)-lipgloss.Width(rightPlain), 0)
	return headerStyle.Render(leftPlain) + strings.Repeat(" ", gap) + headerMetaStyle.Render(rightPlain)
}

// renderHeaderQueryLine shows either the active query (labeled, so it
// reads clearly rather than blending into the rest of the bar) or a
// transient notice in its place — unlabeled, since a notice is already a
// short, self-contained sentence ("saved query x", "project switch
// failed: ...") that a "Query:" label in front of would misdescribe.
func (m appModel) renderHeaderQueryLine() string {
	label, text, style := "Query: ", m.filter.Build(), plainStyle
	if m.notice != "" {
		label, text, style = "", m.notice, errorStyle
	}
	if text == "" {
		text = "(none — showing all entries)"
	}

	// A raw query can embed newlines (Cloud Logging treats them as AND,
	// same as the structured fields' own " AND " join); a real error can
	// contain them too. Collapsing every run of whitespace to a single
	// space and then clipping to what fits keeps this to exactly one
	// physical line no matter what the filter or notice contains — the
	// header silently growing past its budget once pushed the footer
	// past the bottom of the screen (still there and responding to
	// input, just scrolled out of view).
	text = strings.Join(strings.Fields(text), " ")

	renderedLabel := detailLabelStyle.Render(label)
	if avail := m.width - lipgloss.Width(renderedLabel); avail > 0 {
		// Middle-, not head-, truncation: Build() always appends the
		// timestamp clause at the very end, so simple head truncation
		// could never show it — you'd always lose the tail of any query
		// long enough to need truncating at all.
		text = truncateMiddle(text, avail)
	}
	return renderedLabel + style.Render(text)
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
