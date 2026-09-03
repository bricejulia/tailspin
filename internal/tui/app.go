// Package tui is tailspin's Bubble Tea application. It depends only on the
// gcplog package's domain types (Client, Entry, FilterState, TailEvent) —
// never on the underlying GCP SDK — so it can be driven in tests with a
// fake Client and no network access.
package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// mode selects which part of the UI is active. modeDetail and modeTail are
// wired up as mode transitions here (M3) but modeTail's actual live
// streaming lands in M5; modeDetail lands in M4.
type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeFilterFocus
	modeCommand
	modeTail
	modeHelp
	modeError
)

const pageSize = 50

// fetchTimeout bounds a single ListEntries/NewClient call so a stalled
// network request can't hang the UI forever.
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

	list      listModel
	detail    detailModel
	filterBar filterBarModel
	command   commandModel
	tail      tailModel
	spinner   spinner.Model

	// tailGen identifies the current tail session; tailStartedMsg/
	// tailEventMsg carry the gen they belong to, so a message from a
	// session the user has already left (stopped, or started another)
	// is silently ignored instead of resurrecting stale state.
	tailGen    int
	tailCancel func()
	tailEvents <-chan gcplog.TailEvent

	err    error  // fatal: takes over the whole screen (modeError)
	notice string // transient: shown in the header, doesn't change mode

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
		filter:    gcplog.FilterState{Since: time.Now().Add(-defaultLookback)},
		list:      newListModel(),
		detail:    newDetailModel(),
		filterBar: newFilterBarModel(),
		command:   newCommandModel(),
		tail:      newTailModel(),
		// Line ("|/-\") is plain ASCII — no risk of a Braille/block
		// glyph not rendering on some font or terminfo combination (see
		// the ">" selection-marker doc comment in styles.go for a case
		// where that happened with a fancier character).
		spinner: spinner.New(spinner.WithSpinner(spinner.Line), spinner.WithStyle(spinnerStyle)),
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
		m.detail.SetSize(m.width, m.contentHeight())
		m.tail.SetSize(m.width, m.contentHeight())
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case entriesLoadedMsg:
		return m.handleEntriesLoaded(msg)

	case filterSubmittedMsg:
		m.filter = msg.filter
		m.notice = ""
		m.mode = modeBrowse
		return m, m.fetchPage("", false)

	case commandSubmittedMsg:
		return m.handleCommand(msg.cmd)

	case projectSwitchedMsg:
		return m.handleProjectSwitched(msg)

	case tailStartedMsg:
		return m.handleTailStarted(msg)

	case tailEventMsg:
		return m.handleTailEvent(msg)

	case spinner.TickMsg:
		if !m.list.loading {
			return m, nil // fetch already finished — let the animation stop
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleKey routes a key press either to the focused sub-model (in an
// input-capturing mode) or to global/browse handling.
func (m appModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilterFocus:
		if msg.String() == "esc" {
			m.mode = modeBrowse
			return m, nil
		}
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		return m, cmd

	case modeCommand:
		if msg.String() == "esc" {
			m.mode = modeBrowse
			return m, nil
		}
		var cmd tea.Cmd
		m.command, cmd = m.command.Update(msg)
		return m, cmd

	case modeHelp:
		switch msg.String() {
		case "esc", "q", "?":
			m.mode = modeBrowse
		}
		return m, nil

	default:
		// modeBrowse, modeDetail, modeTail, modeError: handled below.
	}

	// modeBrowse, modeTail, modeDetail, modeError: global keys first,
	// then mode-specific handling.
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}

	switch {
	case key.Matches(msg, keys.Filter):
		m.filterBar.seed(m.filter)
		m.mode = modeFilterFocus
		return m, m.filterBar.focusField()
	case key.Matches(msg, keys.Command):
		m.mode = modeCommand
		return m, m.command.focus()
	case key.Matches(msg, keys.Help):
		m.mode = modeHelp
		return m, nil
	case key.Matches(msg, keys.Tail):
		m.mode = modeTail
		return m.startTail()
	case key.Matches(msg, keys.Back):
		if m.mode == modeTail {
			m = m.stopTail()
		}
		if m.mode == modeTail || m.mode == modeDetail || m.mode == modeError {
			m.mode = modeBrowse
			m.err = nil
		}
		return m, nil
	case key.Matches(msg, keys.Enter):
		if m.mode == modeBrowse {
			if entry, ok := m.list.selectedEntry(); ok {
				m.detail.show(entry)
				m.mode = modeDetail
			}
		}
		return m, nil
	case key.Matches(msg, keys.Refresh):
		if m.mode == modeBrowse || m.mode == modeError {
			m.mode = modeBrowse
			m.err = nil
			return m, m.fetchPage("", false)
		}
		return m, nil
	case key.Matches(msg, keys.NextPage):
		if m.mode == modeBrowse {
			if m.list.gotoNextPage() {
				return m, tea.Batch(m.fetchPage(m.list.nextPageToken, true), m.spinner.Tick)
			}
		}
		return m, nil
	case key.Matches(msg, keys.PrevPage):
		if m.mode == modeBrowse {
			m.list.gotoPreviousPage()
		}
		return m, nil
	}

	switch m.mode {
	case modeBrowse:
		var cmd tea.Cmd
		wasLoading := m.list.loading
		m.list, cmd = m.list.Update(msg)
		if !wasLoading && m.list.loading {
			// list_model just flagged that it wants the next page.
			cmd = tea.Batch(cmd, m.fetchPage(m.list.nextPageToken, true), m.spinner.Tick)
		}
		return m, cmd
	case modeDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	case modeTail:
		m.tail = m.tail.Update(msg)
		return m, nil
	default:
		// modeFilterFocus, modeCommand, modeHelp, modeError: no further
		// per-key handling here (the first two return earlier in this
		// function; help and error modes have nothing more to do with an
		// unmatched key).
	}
	return m, nil
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

func (m appModel) handleCommand(cmd parsedCommand) (tea.Model, tea.Cmd) {
	switch cmd.Kind {
	case cmdBrowse:
		if m.mode == modeTail {
			m = m.stopTail()
		}
		m.mode = modeBrowse
		return m, nil
	case cmdTail:
		m.mode = modeTail
		return m.startTail()
	case cmdHelp:
		m.mode = modeHelp
		return m, nil
	case cmdQuit:
		return m, tea.Quit
	case cmdProject:
		m.mode = modeBrowse
		m.notice = "switching to project " + cmd.Arg + "…"
		return m, m.switchProjectCmd(cmd.Arg)
	default:
		// cmdUnknown never reaches here: command_model only emits
		// commandSubmittedMsg for a successfully-parsed command.
	}
	return m, nil
}

func (m appModel) handleProjectSwitched(msg projectSwitchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "project switch failed: " + msg.err.Error()
		return m, nil
	}
	m = m.stopTail()
	if m.client != nil {
		_ = m.client.Close()
	}
	m.client = msg.client
	m.project = msg.project
	m.filter = gcplog.FilterState{Since: time.Now().Add(-defaultLookback)}
	m.notice = ""
	m.mode = modeBrowse
	return m, m.fetchPage("", false)
}

// startTail begins a new tail session: stops any previous one, clears the
// tail view, and kicks off the async open-stream Cmd.
func (m appModel) startTail() (tea.Model, tea.Cmd) {
	m = m.stopTail()
	m.tail.reset()
	return m, m.startTailCmd(m.tailGen)
}

// stopTail cancels the active tail stream, if any, and bumps tailGen so any
// tailStartedMsg/tailEventMsg still in flight for it is ignored on arrival.
func (m appModel) stopTail() appModel {
	if m.tailCancel != nil {
		m.tailCancel()
	}
	m.tailCancel = nil
	m.tailEvents = nil
	m.tailGen++
	return m
}

// startTailCmd opens a live tail stream for gen. Run as a tea.Cmd since
// TailEntries does a blocking RPC call to establish the stream.
func (m appModel) startTailCmd(gen int) tea.Cmd {
	client, filter := m.client, m.filter
	return func() tea.Msg {
		events, cancel, err := client.TailEntries(context.Background(), filter)
		return tailStartedMsg{gen: gen, events: events, cancel: cancel, err: err}
	}
}

// waitForTailEvent returns a Cmd that blocks for the next event on events —
// the standard Bubble Tea "listen on a channel forever" pattern: the tail
// event handler re-issues this after every tailEventMsg it receives, so
// the goroutine reading the channel lives entirely inside Cmds rather than
// writing to shared state from the outside.
func waitForTailEvent(gen int, events <-chan gcplog.TailEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return tailEventMsg{gen: gen, event: ev}
	}
}

func (m appModel) handleTailStarted(msg tailStartedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.tailGen {
		if msg.cancel != nil {
			msg.cancel() // belongs to a session we've already left
		}
		return m, nil
	}
	if msg.err != nil {
		m.notice = "tail failed: " + msg.err.Error()
		m.mode = modeBrowse
		return m, nil
	}
	m.tailCancel = msg.cancel
	m.tailEvents = msg.events
	return m, waitForTailEvent(msg.gen, msg.events)
}

func (m appModel) handleTailEvent(msg tailEventMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.tailGen {
		return m, nil
	}
	if msg.event.Err != nil {
		m.notice = "tail stream ended: " + msg.event.Err.Error()
		return m, nil // don't re-issue the wait — the channel is closed
	}
	m.tail.appendEntry(msg.event.Entry)
	return m, waitForTailEvent(msg.gen, m.tailEvents)
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

// switchProjectCmd returns a Cmd that opens a new gcplog.Client for
// project, driven from the ":project <id>" command.
func (m appModel) switchProjectCmd(project string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		client, err := gcplog.NewClient(ctx, project)
		return projectSwitchedMsg{client: client, project: project, err: err}
	}
}

func (m appModel) View() tea.View {
	var body string
	switch m.mode {
	case modeError:
		body = errorStyle.Render("error: " + friendlyError(m.err))
	case modeHelp:
		body = helpText()
	case modeFilterFocus:
		body = m.list.View()
	case modeCommand:
		body = m.list.View()
	case modeDetail:
		body = m.detail.View()
	case modeTail:
		body = m.tail.View()
	default:
		body = m.list.View()
	}

	var footer string
	switch m.mode {
	case modeFilterFocus:
		footer = m.filterBar.View()
	case modeCommand:
		footer = m.command.View()
	default:
		footer = m.renderFooter()
	}

	content := m.renderHeader() + "\n" + body + "\n" + footer
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
