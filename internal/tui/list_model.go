package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// prefetchThreshold is how close to the bottom of the loaded entries the
// selection must get before the next page is lazily fetched.
const prefetchThreshold = 5

// hscrollStep sizes the h/l horizontal-scroll step to a fraction of the
// viewport width — a flat step of a handful of columns took upwards of a
// hundred presses to cross a long jsonPayload line ("0"/"$" jump straight
// to the ends; this just makes ordinary h/l usable too).
func hscrollStep(width int) int {
	return max(width/3, 10)
}

// gutterWidth is the left column reserved for the selection marker ("> ")
// on the focused row, and matching blank space on every other row so
// columns stay aligned.
const gutterWidth = 2

// listModel renders the scrollable, k9s-dense table of log entries and
// tracks which row is selected. It owns no GCP client — appModel triggers
// fetches and feeds results in via reset/appendPage.
type listModel struct {
	viewport viewport.Model
	entries  []gcplog.Entry
	selected int

	nextPageToken string
	hasMore       bool
	loading       bool

	// wrap controls how rows wider than the terminal are handled: false
	// (the default) keeps every row a single line, full content, and
	// lets h/l ($/0 to jump to the ends) scroll the whole table
	// horizontally to read whatever doesn't fit. true (toggled with 'w')
	// reflows every row across as many lines as its message needs, with
	// continuation lines aligned under the message column — no
	// horizontal scrolling needed, at the cost of the list getting a lot
	// taller.
	wrap bool

	// pageStarts[i] is the entries index where the i-th fetched page
	// begins; len(pageStarts) is how many pages are currently loaded.
	// Pages already fetched are never discarded, so "previous page" is
	// always a free jump within what's already in entries — only
	// crossing past the last loaded page needs a new fetch.
	pageStarts []int
	// pendingPageJump is set when a fetch was kicked off by an explicit
	// next-page request, so once it lands the selection jumps to the
	// start of the newly-loaded page instead of staying put (the
	// implicit near-bottom prefetch, by contrast, never moves selection).
	pendingPageJump bool

	// groupStart[i] is the physical viewport line where entries[i]
	// begins; groupStart[len(entries)] is the total line count. Every
	// row spans exactly one line unless wrap is on (see wrap above).
	groupStart []int

	width, height int
}

func newListModel() listModel {
	return listModel{
		viewport: viewport.New(),
	}
}

func (m *listModel) SetSize(width, height int) {
	m.width, m.height = width, height
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	m.render()
}

// reset replaces the entry set (a fresh query) and scrolls to the top.
func (m *listModel) reset(page gcplog.Page) {
	m.entries = page.Entries
	m.pageStarts = []int{0}
	m.nextPageToken = page.NextPageToken
	m.hasMore = page.NextPageToken != ""
	m.selected = 0
	m.loading = false
	m.pendingPageJump = false
	m.viewport.SetXOffset(0)
	m.render()
	m.viewport.SetYOffset(0)
}

// appendPage extends the entry set with another fetched page. If the fetch
// was triggered by gotoNextPage, selection jumps to the new page's start;
// otherwise (the implicit near-bottom prefetch) selection is left alone.
func (m *listModel) appendPage(page gcplog.Page) {
	newPageStart := len(m.entries)
	m.entries = append(m.entries, page.Entries...)
	m.pageStarts = append(m.pageStarts, newPageStart)
	m.nextPageToken = page.NextPageToken
	m.hasMore = page.NextPageToken != ""
	m.loading = false
	if m.pendingPageJump {
		m.selected = newPageStart
		m.pendingPageJump = false
		m.resetHScrollOnMove()
	}
	m.render()
	m.scrollIntoView()
}

func (m listModel) selectedEntry() (gcplog.Entry, bool) {
	if m.selected < 0 || m.selected >= len(m.entries) {
		return gcplog.Entry{}, false
	}
	return m.entries[m.selected], true
}

// selectedRowEndOffset is the horizontal scroll offset that puts the end of
// the focused row's own content flush with the right edge — used by the
// "$" h-scroll shortcut. Deliberately not the viewport's overall
// longest-loaded-line width: jumping to that instead left a short selected
// row entirely blank whenever some unrelated row happened to be longer.
func (m listModel) selectedRowEndOffset() int {
	e, ok := m.selectedEntry()
	if !ok {
		return 0
	}
	full := gutter(true) + entryPrefix(e) + e.Summary
	return lipgloss.Width(full) - m.width
}

// currentPageIndex returns which loaded page (an index into pageStarts)
// the current selection falls in.
func (m listModel) currentPageIndex() int {
	idx := 0
	for i, start := range m.pageStarts {
		if start <= m.selected {
			idx = i
		}
	}
	return idx
}

// pageNumber and pageCount describe pagination state for display: 1-based
// current page, and how many pages are loaded so far (this only ever
// grows — nothing is evicted — so it's "pages seen", not "total pages").
func (m listModel) pageNumber() int { return m.currentPageIndex() + 1 }
func (m listModel) pageCount() int  { return len(m.pageStarts) }

// gotoNextPage jumps to the start of the next page. If that page is
// already loaded, it jumps immediately and returns false (no fetch
// needed). Otherwise, if more pages exist on the server, it flags the
// jump as pending and returns true — the caller should issue a fetch, and
// appendPage will complete the jump once the page arrives.
func (m *listModel) gotoNextPage() bool {
	idx := m.currentPageIndex()
	if idx+1 < len(m.pageStarts) {
		m.selected = m.pageStarts[idx+1]
		m.resetHScrollOnMove()
		m.render()
		m.scrollIntoView()
		return false
	}
	if m.hasMore && !m.loading {
		m.loading = true
		m.pendingPageJump = true
		return true
	}
	return false
}

// gotoPreviousPage jumps to the start of the previous page. Always free —
// every page ever fetched is still in entries.
func (m *listModel) gotoPreviousPage() {
	idx := m.currentPageIndex()
	if idx == 0 {
		m.selected = 0
	} else {
		m.selected = m.pageStarts[idx-1]
	}
	m.resetHScrollOnMove()
	m.render()
	m.scrollIntoView()
}

// Update handles browse-mode navigation. It returns a Cmd to prefetch the
// next page when the selection nears the end of what's loaded.
func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.Down):
			m.moveSelection(1)
		case key.Matches(keyMsg, keys.Up):
			m.moveSelection(-1)
		case key.Matches(keyMsg, keys.PageDown):
			m.moveSelection(m.height)
		case key.Matches(keyMsg, keys.PageUp):
			m.moveSelection(-m.height)
		case key.Matches(keyMsg, keys.Wrap):
			m.wrap = !m.wrap
			m.viewport.SetXOffset(0)
			m.render()
			m.scrollIntoView()
		case !m.wrap && (keyMsg.String() == "left" || keyMsg.String() == "h"):
			m.viewport.ScrollLeft(hscrollStep(m.width))
		case !m.wrap && (keyMsg.String() == "right" || keyMsg.String() == "l"):
			m.viewport.ScrollRight(hscrollStep(m.width))
		case !m.wrap && (keyMsg.String() == "0" || keyMsg.String() == "home"):
			m.viewport.SetXOffset(0)
		case !m.wrap && (keyMsg.String() == "$" || keyMsg.String() == "end"):
			// The end of the *focused* row's own content, not the
			// viewport's globally-longest loaded line — jumping to the
			// latter left a short selected row entirely blank whenever
			// some unrelated row happened to be much longer.
			m.viewport.SetXOffset(m.selectedRowEndOffset())
		}
	}

	if m.wantsPrefetch() {
		m.loading = true
		return m, nil // appModel.Update issues the actual fetch Cmd
	}
	return m, nil
}

func (m *listModel) moveSelection(delta int) {
	if len(m.entries) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.entries) {
		m.selected = len(m.entries) - 1
	}
	m.resetHScrollOnMove()
	m.render()
	m.scrollIntoView()
}

// resetHScrollOnMove snaps the horizontal scroll position back to the left
// edge whenever the selection moves in non-wrap mode: each row can be a
// completely different length, so a horizontal offset scrolled into one
// row's content isn't meaningful once you've moved to another.
func (m *listModel) resetHScrollOnMove() {
	if !m.wrap {
		m.viewport.SetXOffset(0)
	}
}

// scrollIntoView scrolls the viewport by the minimum amount needed to bring
// the selected entry's full line range into view, nudging one row at a
// time when moving off either edge (rather than viewport.EnsureVisible's
// behavior of re-anchoring the target line to the top on every call, which
// both resets horizontal scroll on every vertical move and, when moving
// down past the bottom edge, jumps a full page instead of one line).
func (m *listModel) scrollIntoView() {
	if len(m.groupStart) <= m.selected+1 {
		return
	}
	top, bottom := m.groupStart[m.selected], m.groupStart[m.selected+1]-1

	switch {
	case top < m.viewport.YOffset():
		m.viewport.SetYOffset(top)
	case bottom >= m.viewport.YOffset()+m.height:
		m.viewport.SetYOffset(bottom - m.height + 1)
	}
}

// wantsPrefetch reports whether the selection is close enough to the loaded
// edge that appModel should kick off a background fetch of the next page.
func (m listModel) wantsPrefetch() bool {
	return m.hasMore && !m.loading && len(m.entries)-m.selected <= prefetchThreshold
}

func (m *listModel) render() {
	m.groupStart = make([]int, len(m.entries)+1)
	if len(m.entries) == 0 {
		m.viewport.SetContent("")
		return
	}

	var lines []string
	for i, e := range m.entries {
		m.groupStart[i] = len(lines)
		lines = append(lines, m.renderRow(i, e)...)
	}
	m.groupStart[len(m.entries)] = len(lines)
	m.viewport.SetContentLines(lines)
}

// severityColWidth fits the longest severity name ("EMERGENCY").
const severityColWidth = 9

// entryPrefix renders the fixed-width timestamp/severity/log-name columns
// shared by every row-based view (the browse list and the tail stream).
//
// It never applies lipgloss's Style.Width to a string that might already be
// at or over that width: Width(n) doesn't just pad short content, it wraps
// (reflows) anything longer than n — exactly the multi-line-row bug this
// was built to avoid. Columns are padded manually with padToWidth instead,
// and styles are applied with plain Render (no Width) throughout.
func entryPrefix(e gcplog.Entry) string {
	sevStyle := severityStyle(e.Severity)
	return fmt.Sprintf("%s  %s  %s  ",
		e.Timestamp.Local().Format("15:04:05"),
		sevStyle.Render(padToWidth(strings.ToUpper(e.Severity.String()), severityColWidth)),
		lipgloss.NewStyle().Foreground(colorMuted).Render(padToWidth(truncate(shortLogName(e.LogName), 28), 28)),
	)
}

// gutter renders the 2-column left margin: a colored marker on the
// selected row, blank space otherwise, so the selection is visible even
// where the background tint alone is subtle.
func gutter(selected bool) string {
	if selected {
		return selectedMarkerStyle.Render(">") + " "
	}
	return strings.Repeat(" ", gutterWidth)
}

// renderRow renders entry i as one or more physical lines: every row wraps
// (or none do) together — see the wrap field doc for the two modes.
func (m listModel) renderRow(i int, e gcplog.Entry) []string {
	selected := i == m.selected
	prefix := gutter(selected) + entryPrefix(e)
	prefixWidth := lipgloss.Width(prefix)

	var rowLines []string
	if m.wrap {
		// Every row reflows across as many lines as its message needs,
		// continuation lines aligned under the message column.
		avail := max(m.width-prefixWidth, 10)
		indent := strings.Repeat(" ", prefixWidth)
		for j, seg := range strings.Split(lipgloss.Wrap(e.Summary, avail, ""), "\n") {
			if j == 0 {
				rowLines = append(rowLines, prefix+seg)
			} else {
				rowLines = append(rowLines, indent+seg)
			}
		}
	} else {
		// Every row is one line, full content — h/l (0/$) scrolls the
		// whole table horizontally to read whatever doesn't fit.
		rowLines = []string{prefix + e.Summary}
	}

	if selected {
		for j, line := range rowLines {
			rowLines[j] = selectedRowStyle.Render(padToWidth(line, m.width))
		}
	}
	return rowLines
}

// padToWidth right-pads s with spaces to width display columns. Content
// already at or beyond width is returned unchanged (never truncated —
// callers that need clipping do that separately).
func padToWidth(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func (m listModel) View() string {
	if len(m.entries) == 0 {
		if m.loading {
			return statusStyle.Render("loading…")
		}
		return statusStyle.Render("no log entries match the current filter")
	}
	return m.viewport.View()
}

// shortLogName trims a full log resource name
// ("projects/p/logs/syslog") down to just the log ID.
func shortLogName(logName string) string {
	if idx := strings.LastIndex(logName, "/logs/"); idx != -1 {
		return logName[idx+len("/logs/"):]
	}
	return logName
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
