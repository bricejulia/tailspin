package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// tailMaxEntries bounds how many entries a tail session keeps in memory;
// past that, the oldest are dropped as new ones arrive.
const tailMaxEntries = 5000

// pauseKey toggles autoscroll in tail mode. Not part of keyMap (so it
// doesn't show up outside modeTail's own footer).
var pauseKey = key.NewBinding(key.WithKeys("space", "p"), key.WithHelp("space/p", "pause"))

// tailModel renders a live-appending stream of entries. It owns no GCP
// client or goroutine of its own — appModel starts/stops the underlying
// stream (see startTailCmd/waitForTailEvent) and feeds entries in via
// appendEntry, keeping the same Msg/Cmd-driven architecture as everything
// else rather than a background goroutine writing to shared state.
type tailModel struct {
	viewport viewport.Model
	entries  []gcplog.Entry
	paused   bool
}

func newTailModel() tailModel {
	return tailModel{viewport: viewport.New()}
}

func (m *tailModel) SetSize(width, height int) {
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
}

// reset clears the stream for a fresh tail session.
func (m *tailModel) reset() {
	m.entries = nil
	m.paused = false
	m.viewport.SetContent("")
}

// appendEntry adds a newly-arrived entry. Entries keep arriving and are
// kept even while paused — pause only stops autoscroll, so nothing is lost
// while you're reading an earlier point in the stream.
func (m *tailModel) appendEntry(e gcplog.Entry) {
	m.entries = append(m.entries, e)
	if over := len(m.entries) - tailMaxEntries; over > 0 {
		m.entries = m.entries[over:]
	}

	lines := make([]string, len(m.entries))
	for i, entry := range m.entries {
		lines[i] = entryPrefix(entry) + entry.Summary
	}
	m.viewport.SetContentLines(lines)
	if !m.paused {
		m.viewport.GotoBottom()
	}
}

func (m tailModel) Update(msg tea.Msg) tailModel {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, pauseKey):
			m.paused = !m.paused
			if !m.paused {
				m.viewport.GotoBottom()
			}
		case key.Matches(keyMsg, keys.Up):
			m.viewport.ScrollUp(1)
			m.paused = true
		case key.Matches(keyMsg, keys.Down):
			m.viewport.ScrollDown(1)
		case key.Matches(keyMsg, keys.PageUp):
			m.viewport.PageUp()
			m.paused = true
		case key.Matches(keyMsg, keys.PageDown):
			m.viewport.PageDown()
		}
	}
	return m
}

func (m tailModel) View() string {
	if len(m.entries) == 0 {
		// Pad to the full allocated box, same as viewport.View() does for
		// its content — otherwise this collapses to one line and whatever
		// follows (the footer) ends up right underneath it instead of
		// pinned to the bottom of the terminal.
		return lipgloss.NewStyle().Width(m.viewport.Width()).Height(m.viewport.Height()).
			Render(statusStyle.Render("waiting for log entries…"))
	}
	return m.viewport.View()
}
