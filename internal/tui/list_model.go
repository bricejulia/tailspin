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
	m.viewport.GotoTop()
	m.render()
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
	m.viewport.EnsureVisible(m.selected, 0, 0)
	m.render()
}

// wantsPrefetch reports whether the selection is close enough to the loaded
// edge that appModel should kick off a background fetch of the next page.
func (m listModel) wantsPrefetch() bool {
	return m.hasMore && !m.loading && len(m.entries)-m.selected <= prefetchThreshold
}

func (m *listModel) render() {
	if len(m.entries) == 0 {
		m.viewport.SetContent("")
		return
	}
	lines := make([]string, len(m.entries))
	for i, e := range m.entries {
		lines[i] = m.renderRow(i, e)
	}
	m.viewport.SetContentLines(lines)
}

func (m listModel) renderRow(i int, e gcplog.Entry) string {
	sevStyle := severityStyle(e.Severity)
	row := fmt.Sprintf("%s  %s  %s  %s",
		e.Timestamp.Local().Format("15:04:05"),
		sevStyle.Width(8).Render(strings.ToUpper(e.Severity.String())),
		lipgloss.NewStyle().Foreground(colorMuted).Width(28).Render(truncate(shortLogName(e.LogName), 28)),
		e.Summary,
	)
	if i == m.selected {
		return selectedRowStyle.Width(m.width).Render(row)
	}
	return row
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
