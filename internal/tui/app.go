// Package tui is tailspin's Bubble Tea application. It depends only on the
// gcplog package's domain types (Client, Entry, FilterState, TailEvent) —
// never on the underlying GCP SDK — so it can be driven in tests with a
// fake Client and no network access.
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// mode selects which part of the UI is active. Only modeBrowse is fully
// wired up in this milestone; the rest land in later milestones.
type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeFilterFocus
	modeCommand
	modeTail
	modeError
)

const pageSize = 50

// fetchTimeout bounds a single ListEntries call so a stalled network
// request can't hang the UI forever.
const fetchTimeout = 15 * time.Second

// defaultLookback is how far back browse mode looks by default, matching
// logadmin's own default (see the FilterState.Since doc comment for why
// this must be resolved once, not left for logadmin to inject per call).
const defaultLookback = 24 * time.Hour

// appModel is the top-level Bubble Tea model.
type appModel struct {
	mode mode

	client  gcplog.Client
	project string
	filter  gcplog.FilterState

	list listModel

	err error

	width, height int
}

// New builds tailspin's top-level model for the given project and client.
func New(client gcplog.Client, project string) appModel {
	return appModel{
		client:  client,
		project: project,
		// Resolved once, here — not left zero for logadmin to default
		// per-request from time.Now(), which would make the filter
		// string drift between pages of the same paginated query and
		// break the page token (see FilterState.Since).
		filter: gcplog.FilterState{Since: time.Now().Add(-defaultLookback)},
		list:   newListModel(),
	}
}

func (m appModel) Init() tea.Cmd {
	return m.fetchPage("", false)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.width, m.contentHeight())
		return m, nil

	case tea.KeyPressMsg:
		if cmd, handled := m.handleGlobalKey(msg); handled {
			return m, cmd
		}

	case entriesLoadedMsg:
		return m.handleEntriesLoaded(msg)
	}

	switch m.mode {
	case modeBrowse:
		var cmd tea.Cmd
		wasLoading := m.list.loading
		m.list, cmd = m.list.Update(msg)
		if !wasLoading && m.list.loading {
			// list_model just flagged that it wants the next page.
			cmd = tea.Batch(cmd, m.fetchPage(m.list.nextPageToken, true))
		}
		return m, cmd
	}

	return m, nil
}

// handleGlobalKey handles keys that apply regardless of mode (quit, and —
// once modeError is entered — nothing else). Mode-specific keys are handled
// by the sub-model's own Update.
func (m appModel) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit, true
	}
	return nil, false
}

func (m appModel) handleEntriesLoaded(msg entriesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.mode = modeError
		return m, nil
	}
	if msg.appending {
		m.list.appendPage(msg.page)
	} else {
		m.list.reset(msg.page)
	}
	return m, nil
}

// contentHeight is the terminal height available to the list/detail view,
// after reserving one line each for the header and footer.
func (m appModel) contentHeight() int {
	h := m.height - 2
	if h < 0 {
		return 0
	}
	return h
}

// fetchPage returns a Cmd that lists one page of entries. appending
// controls whether the result should extend the current list (pagination)
// or replace it (a fresh query).
func (m appModel) fetchPage(pageToken string, appending bool) tea.Cmd {
	client, filter := m.client, m.filter
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		page, err := client.ListEntries(ctx, filter, pageToken, pageSize)
		return entriesLoadedMsg{page: page, appending: appending, err: err}
	}
}

func (m appModel) View() tea.View {
	if m.mode == modeError && m.err != nil {
		v := tea.NewView(m.renderHeader() + "\n" + errorStyle.Render("error: "+m.err.Error()) + "\n" + m.renderFooter())
		v.AltScreen = true
		return v
	}

	body := m.list.View()
	content := m.renderHeader() + "\n" + body + "\n" + m.renderFooter()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
