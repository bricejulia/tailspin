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

// listModel renders the scrollable, k9s-dense table of log entries and
// tracks which row is selected. It owns no GCP client — appModel triggers
// fetches and feeds results in via setEntries/appendEntries.
type listModel struct {
	viewport viewport.Model
	entries  []gcplog.Entry
	selected int

	nextPageToken string
	hasMore       bool
	loading       bool

	// wrap controls how rows wider than the terminal are handled:
	// false (the default, k9s-style) keeps one entry = one screen line
	// and lets h/l (or ←/→) scroll the table horizontally to read the
	// rest. true reflows each entry's message across as many lines as
	// it needs, with continuation lines aligned under the message
	// column. Toggled with 'w'.
	wrap bool

	// groupStart[i] is the physical viewport line where entries[i]
	// begins; groupStart[len(entries)] is the total line count. In
	// non-wrap mode this is just the identity (one line per entry); in
	// wrap mode an entry can span several lines.
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
	m.nextPageToken = page.NextPageToken
	m.hasMore = page.NextPageToken != ""
	m.selected = 0
	m.loading = false
	m.viewport.SetXOffset(0)
	m.render()
	m.viewport.SetYOffset(0)
}

// appendPage extends the entry set with another fetched page.
func (m *listModel) appendPage(page gcplog.Page) {
	m.entries = append(m.entries, page.Entries...)
	m.nextPageToken = page.NextPageToken
	m.hasMore = page.NextPageToken != ""
	m.loading = false
	m.render()
}

func (m listModel) selectedEntry() (gcplog.Entry, bool) {
	if m.selected < 0 || m.selected >= len(m.entries) {
		return gcplog.Entry{}, false
	}
	return m.entries[m.selected], true
}

// Update handles browse-mode navigation. It returns a Cmd to prefetch the
// next page when the selection nears the end of what's loaded.
func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.Down):
			m.moveSelection(1)
		case key.Matches(msg, keys.Up):
			m.moveSelection(-1)
		case key.Matches(msg, keys.PageDown):
			m.moveSelection(m.height)
		case key.Matches(msg, keys.PageUp):
			m.moveSelection(-m.height)
		case key.Matches(msg, keys.Wrap):
			m.wrap = !m.wrap
			m.viewport.SetXOffset(0)
			m.render()
			m.scrollIntoView()
		case !m.wrap && (msg.String() == "left" || msg.String() == "h"):
			m.viewport.ScrollLeft(4)
		case !m.wrap && (msg.String() == "right" || msg.String() == "l"):
			m.viewport.ScrollRight(4)
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
	m.render()
	m.scrollIntoView()
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

// renderRow renders entry i as one or more physical lines, depending on
// wrap mode.
func (m listModel) renderRow(i int, e gcplog.Entry) []string {
	prefix := entryPrefix(e)
	prefixWidth := lipgloss.Width(prefix)

	var rowLines []string
	if m.wrap {
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
		// One entry = one screen line, full width. 'w' is off — h/l
		// horizontally scrolls the viewport to read the rest rather
		// than truncating it away.
		rowLines = []string{prefix + e.Summary}
	}

	if i == m.selected {
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
