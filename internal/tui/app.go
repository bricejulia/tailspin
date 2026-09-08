// Package tui is tailspin's Bubble Tea application. It depends only on the
// gcplog package's domain types (Client, Entry, FilterState, TailEvent) —
// never on the underlying GCP SDK — so it can be driven in tests with a
// fake Client and no network access.
package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bricejulia/tailspin/internal/config"
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
	modeFacetFocus // the facet side panel has keyboard focus (see toggleFacets)
	modeCommand
	modeTail
	modeHelp
	modeError
	modeQuery   // ":query"'s full-screen raw/advanced filter editor
	modeQueries // ":queries"'s static list of saved favorites
)

const pageSize = 50

// fetchTimeout bounds a single ListEntries/NewClient call so a stalled
// network request can't hang the UI forever.
const fetchTimeout = 15 * time.Second

// histogramTimeout bounds a single Histogram call. Much longer than
// fetchTimeout: every read request a histogram fetch makes is now paced by
// the configured read-requests-per-minute budget (see
// internal/config.ResolveReadQuota, gcplog.Client's readLimiter) rather than
// fired freely, so a histogram spanning many buckets/pages can legitimately
// take a while at the default 60/minute quota. A bucket that's still
// waiting on that pacing when this expires just renders as incomplete (see
// gcplog.HistogramBucket.Capped) rather than failing outright — this bounds
// how long that takes to happen, not how much data can be fetched.
const histogramTimeout = 2 * time.Minute

// facetsTimeout bounds a single Client.Facets call — see histogramTimeout's
// doc comment above for why this needs to be much longer than fetchTimeout
// now that every read request is paced by the configured read-quota
// budget.
const facetsTimeout = histogramTimeout

// facetPanelWidth is the facet side panel's fixed column width whenever
// it's open (see browseContentWidth).
const facetPanelWidth = 34

// histogramResizeDebounce is how long a resize-triggered histogram refetch
// waits for the terminal size to stop changing before actually firing (see
// the WindowSizeMsg case) — a resize commonly delivers a burst of several
// WindowSizeMsgs, and only the final size in that burst is worth a fetch.
// A var, not a const, so tests can shrink it rather than actually waiting.
var histogramResizeDebounce = 400 * time.Millisecond

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
	query     queryModel
	histogram histogramModel
	facets    facetModel
	spinner   spinner.Model

	// savedQueries is populated by ":queries" for modeQueries' View to
	// render; queriesSelected is the highlighted entry within it, reset
	// to 0 each time the list is (re)loaded.
	savedQueries    []config.SavedQuery
	queriesSelected int

	// tailGen identifies the current tail session; tailStartedMsg/
	// tailEventMsg carry the gen they belong to, so a message from a
	// session the user has already left (stopped, or started another)
	// is silently ignored instead of resurrecting stale state.
	tailGen    int
	tailCancel func()
	tailEvents <-chan gcplog.TailEvent

	// histogramResizeGen identifies the latest resize-triggered histogram
	// refetch requested; a histogramResizeSettledMsg carrying a stale gen
	// (a later resize has since bumped it) is ignored — the debounce
	// mechanism for the WindowSizeMsg case (see histogramResizeDebounce).
	histogramResizeGen int

	// facetGen identifies the current facet fetch; a facetsLoadedMsg
	// carrying a stale gen (the filter changed, or the panel was
	// closed/reopened since) is ignored — the same staleness guard tailGen
	// gives tailStartedMsg/tailEventMsg.
	facetGen int

	err    error  // fatal: takes over the whole screen (modeError)
	notice string // transient: shown in the header, doesn't change mode

	width, height int

	// readQuota is the read-requests-per-minute budget passed to
	// gcplog.NewClient when opening a new client for a ":project <id>"
	// switch (see switchProjectCmd) — the client passed to New already has
	// its own budget baked into it by the caller, so this only matters
	// later. Set via SetReadQuota; zero falls back to
	// config.DefaultReadQuota.
	readQuota int
}

// SetReadQuota configures the read-requests-per-minute budget appModel uses
// when opening a new gcplog.Client after a ":project <id>" switch. main.go
// calls this once, right after New, with whatever config.ResolveReadQuota
// resolved for the client New was given — kept as a separate setter rather
// than a New parameter so every existing New(client, project) call site
// (throughout this package's tests) didn't need updating for a value that,
// for them, is irrelevant (they never switch projects).
func (m *appModel) SetReadQuota(n int) {
	m.readQuota = n
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
		list: func() listModel {
			l := newListModel()
			l.loading = true // Init's fetch starts immediately; see triggerFetch
			return l
		}(),
		detail:    newDetailModel(),
		filterBar: newFilterBarModel(),
		command:   newCommandModel(),
		tail:      newTailModel(),
		query:     newQueryModel(),
		histogram: newHistogramModel(),
		facets:    newFacetModel(),
		// Line ("|/-\") is plain ASCII — no risk of a Braille/block
		// glyph not rendering on some font or terminfo combination (see
		// the ">" selection-marker doc comment in styles.go for a case
		// where that happened with a fancier character).
		spinner: spinner.New(spinner.WithSpinner(spinner.Line), spinner.WithStyle(spinnerStyle)),
	}
}

func (m appModel) Init() tea.Cmd {
	// list.loading is already true from New() — just start the fetch and
	// the spinner tick loop together.
	return tea.Batch(m.fetchPage("", false), m.spinner.Tick)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Column boundaries are about to shift under the cursor — discard
		// rather than remap an in-progress drag, the same "resize just
		// re-renders from current state" treatment everything else here gets.
		m.histogram.cancelDrag()
		m.applySizes()
		if m.showsHistogram() && histogramBucketCount(m.width) != m.histogram.fetchedForBuckets {
			// Debounced, not fetched immediately: a terminal resize — a
			// window drag, a multiplexer reflow — commonly delivers a burst
			// of WindowSizeMsgs in quick succession, and each histogram
			// fetch is itself several GCP API requests. Firing one per
			// event risks tripping a project's read-request-rate quota for
			// no benefit, since only the last size in the burst matters.
			m.histogramResizeGen++
			return m, tea.Tick(histogramResizeDebounce, func(time.Time) tea.Msg {
				return histogramResizeSettledMsg{gen: m.histogramResizeGen}
			})
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

	case entriesLoadedMsg:
		return m.handleEntriesLoaded(msg)

	case filterSubmittedMsg:
		m.filter = msg.filter
		m.notice = ""
		m.mode = modeBrowse
		return m.triggerFetchAndHistogram("", false)

	case commandSubmittedMsg:
		return m.handleCommand(msg.cmd)

	case querySubmittedMsg:
		return m.applyRawQuery(msg.rawQuery)

	case projectSwitchedMsg:
		return m.handleProjectSwitched(msg)

	case tailStartedMsg:
		return m.handleTailStarted(msg)

	case tailEventMsg:
		return m.handleTailEvent(msg)

	case histogramLoadedMsg:
		return m.handleHistogramLoaded(msg)

	case histogramRangeSelectedMsg:
		return m.handleHistogramRangeSelected(msg)

	case histogramResizeSettledMsg:
		if msg.gen != m.histogramResizeGen {
			return m, nil // superseded by a later resize
		}
		if !m.showsHistogram() {
			return m, nil // resized again, e.g. into a mode/width that hides it
		}
		m.histogram.setLoading()
		return m, m.fetchHistogramCmd()

	case facetsLoadedMsg:
		return m.handleFacetsLoaded(msg)

	case facetValueSelectedMsg:
		return m.applyFacetFilter(msg.row)

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

	case modeFacetFocus:
		switch {
		case msg.String() == "esc" || msg.String() == "q":
			// Unfocus only — the panel stays open, showing its last
			// counts, until the toggle key closes it outright.
			m.mode = m.facets.returnMode
			return m, nil
		case key.Matches(msg, keys.Facets):
			return m.closeFacets()
		case key.Matches(msg, keys.Refresh):
			return m.triggerFacetFetch()
		}
		var cmd tea.Cmd
		m.facets, cmd = m.facets.Update(msg)
		return m, cmd

	case modeQuery:
		if msg.String() == "esc" {
			m.mode = modeBrowse
			return m, nil
		}
		var cmd tea.Cmd
		m.query, cmd = m.query.Update(msg)
		return m, cmd

	case modeQueries:
		switch msg.String() {
		case "esc", "q":
			m.mode = modeBrowse
			return m, nil
		case "j", "down":
			m.moveQueriesSelection(1)
			return m, nil
		case "k", "up":
			m.moveQueriesSelection(-1)
			return m, nil
		case "enter":
			return m.runSelectedQuery()
		}
		return m, nil

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
	case key.Matches(msg, keys.Facets):
		if m.mode == modeBrowse || m.mode == modeTail {
			return m.openOrFocusFacets()
		}
		return m, nil
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
			return m.triggerFetchAndHistogram("", false)
		}
		return m, nil
	case key.Matches(msg, keys.NextPage):
		if m.mode == modeBrowse {
			if m.list.gotoNextPage() {
				return m.triggerFetch(m.list.nextPageToken, true)
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
			var fetchCmd tea.Cmd
			m, fetchCmd = m.triggerFetch(m.list.nextPageToken, true)
			cmd = tea.Batch(cmd, fetchCmd)
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

// handlePaste routes a bracketed-paste event to whichever sub-model is
// currently editable, the same three modes handleKey special-cases for
// text input. Without this, a paste (tea.PasteMsg, not a stream of
// tea.KeyPressMsg) never reached any sub-model at all — the underlying
// textinput/textarea widgets both handle tea.PasteMsg correctly, this was
// purely a missing routing case here.
func (m appModel) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilterFocus:
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		return m, cmd
	case modeCommand:
		var cmd tea.Cmd
		m.command, cmd = m.command.Update(msg)
		return m, cmd
	case modeQuery:
		var cmd tea.Cmd
		m.query, cmd = m.query.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
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
	case cmdQuery:
		m.query.seed(m.filter.RawQuery)
		m.mode = modeQuery
		return m, m.query.focus()
	case cmdSave:
		return m.saveCurrentQuery(cmd.Arg)
	case cmdLoad:
		return m.loadSavedQuery(cmd.Arg)
	case cmdQueries:
		return m.showSavedQueries()
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
	m.histogram.reset() // the previous project's counts don't apply here
	m.facets.reset()
	m.facetGen++ // invalidate any facet fetch still in flight for the old project
	m.notice = ""
	m.mode = modeBrowse
	return m.triggerFetchAndHistogram("", false)
}

// startTail begins a new tail session: stops any previous one, clears the
// tail view, and kicks off the async open-stream Cmd. The histogram is
// fetched once here too — a point-in-time snapshot up to "now" (see
// fetchHistogramCmd), not live-recomputed per streamed entry. The facet
// panel, if open, follows the same discipline (see facetSourceIsTail):
// reset here rather than recomputed, since the tail buffer it would
// aggregate over was just cleared too — the user refreshes it explicitly
// (keys.Refresh) once new entries have streamed in.
func (m appModel) startTail() (tea.Model, tea.Cmd) {
	m = m.stopTail()
	m.tail.reset()
	m.histogram.reset()
	m.histogram.setLoading()
	if m.facets.open {
		m.facets.reset()
	}
	return m, tea.Batch(m.startTailCmd(m.tailGen), m.fetchHistogramCmd())
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

// contentHeight is the terminal height available to the detail/query view,
// after reserving headerLines for the header (see renderHeader) and one
// line for the footer. detail and query never show the histogram (see
// showsHistogram), so they get the full budget — browseContentHeight is the
// one to use for anything that does.
func (m appModel) contentHeight() int {
	h := m.height - headerLines - 1
	if h < 0 {
		return 0
	}
	return h
}

// browseContentHeight is contentHeight() minus the histogram's fixed row
// budget, whenever the histogram is actually shown (see showsHistogram) —
// used by list and tail, the only two sub-models whose body is ever
// rendered under the histogram.
func (m appModel) browseContentHeight() int {
	h := m.contentHeight()
	if m.showsHistogram() {
		h -= histogramLines
	}
	if h < 0 {
		return 0
	}
	return h
}

// browseContentWidth is the terminal width available to the list/tail
// body, after reserving facetPanelWidth for the facet side panel whenever
// it's open (see toggleFacets) — the horizontal analog of
// browseContentHeight's vertical reservation for the histogram. Floored
// well above zero so a pathologically narrow terminal with the panel open
// doesn't hand the list a negative or unusably tiny width.
func (m appModel) browseContentWidth() int {
	if !m.facets.open {
		return m.width
	}
	w := m.width - facetPanelWidth - 1 // -1 for the panel's left border column
	if w < 20 {
		return 20
	}
	return w
}

// applySizes propagates the current terminal size, and whether the facet
// panel is open, to every sub-model. Called both from the WindowSizeMsg
// handler and from openOrFocusFacets/closeFacets, since opening or closing
// the panel changes the list/tail width split immediately — it can't wait
// for the next actual terminal resize.
func (m *appModel) applySizes() {
	m.list.SetSize(m.browseContentWidth(), m.browseContentHeight())
	m.detail.SetSize(m.width, m.contentHeight())
	m.tail.SetSize(m.browseContentWidth(), m.browseContentHeight())
	m.query.SetSize(m.width, m.contentHeight())
	m.histogram.SetSize(m.width, histogramLines)
	if m.facets.open {
		m.facets.SetSize(facetPanelWidth, m.browseContentHeight())
	}
}

// showsHistogram reports whether the current mode's body is the log
// list/tail view (as opposed to detail, query, help, error, or the saved
// queries screen) and the terminal is wide enough for a meaningful chart.
func (m appModel) showsHistogram() bool {
	switch m.mode {
	case modeBrowse, modeFilterFocus, modeCommand, modeTail:
		return m.width >= minHistogramWidth
	default:
		// modeDetail, modeQuery, modeHelp, modeError, modeQueries: body is
		// never the list/tail view here, so there's nothing for the chart
		// to sit above.
		return false
	}
}

// histogramRowRange returns the inclusive screen row range the histogram
// occupies: right after the fixed-height header, for exactly histogramLines
// rows — trivial since renderHeader always renders exactly headerLines
// lines.
func (m appModel) histogramRowRange() (top, bottom int) {
	top = headerLines
	return top, top + histogramLines - 1
}

// histogramColumnAt maps a screen x-coordinate to a bucket index (see
// bucketForColumn — a bucket commonly spans more than one screen column,
// since gcplog.MaxHistogramBuckets caps request volume well below a wide
// terminal's column count).
func (m appModel) histogramColumnAt(x int) int {
	n := len(m.histogram.columns)
	if n == 0 {
		return 0
	}
	return bucketForColumn(min(max(x, 0), m.width-1), m.width, n)
}

// histogramBucketCount is how many buckets fetchHistogramCmd requests for
// the given terminal width: enough to fill it column-for-column up to
// gcplog.MaxHistogramBuckets, capped there regardless of width — beyond
// that cap, a wider terminal just renders each bucket across more columns
// (see bucketForColumn) rather than requesting more of them. Request
// volume, not display resolution, is what the cap protects (see
// gcplog.MaxHistogramBuckets' doc comment).
func histogramBucketCount(width int) int {
	if width < 1 {
		return 0
	}
	return min(width, gcplog.MaxHistogramBuckets)
}

// handleMouse routes a mouse event to the histogram, the only
// mouse-interactive part of the UI. Events outside the chart's row range,
// or while it isn't shown at all, are ignored.
func (m appModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.showsHistogram() {
		return m, nil
	}
	mouse := msg.Mouse()
	top, bottom := m.histogramRowRange()

	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft || mouse.Y < top || mouse.Y > bottom {
			return m, nil
		}
		m.histogram.beginDrag(m.histogramColumnAt(mouse.X))
		return m, nil
	case tea.MouseMotionMsg:
		if !m.histogram.dragging {
			return m, nil
		}
		m.histogram.updateDrag(m.histogramColumnAt(mouse.X))
		return m, nil
	case tea.MouseReleaseMsg:
		if !m.histogram.dragging {
			return m, nil
		}
		return m, m.histogram.endDrag(m.histogramColumnAt(mouse.X))
	default:
		// tea.MouseWheelMsg: not handled — reserved for a possible future
		// zoom, not part of the drag-to-select gesture.
		return m, nil
	}
}

func (m appModel) handleHistogramLoaded(msg histogramLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.histogram.setErr(msg.err)
		return m, nil
	}
	m.histogram.setResult(msg.result, msg.forBuckets)
	return m, nil
}

// handleHistogramRangeSelected applies a time range picked by clicking or
// dragging on the histogram. A bounded range is fundamentally incompatible
// with tail's live, unbounded stream, so this stops tailing first — the
// same tail-is-exclusive precedent keys.Back already sets (see there) —
// before falling back to browse with the selected range applied.
func (m appModel) handleHistogramRangeSelected(msg histogramRangeSelectedMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeTail {
		m = m.stopTail()
	}
	m.filter.Since, m.filter.Until = msg.since, msg.until
	m.notice = ""
	m.mode = modeBrowse
	return m.triggerFetchAndHistogram("", false)
}

// openOrFocusFacets opens the facet panel (fetching fresh counts) if it's
// currently closed, or just gives it keyboard focus if it's already open
// but unfocused (e.g. the user pressed esc/q earlier without closing it
// outright) — counts aren't stale in that case, so no refetch.
func (m appModel) openOrFocusFacets() (appModel, tea.Cmd) {
	wasOpen := m.facets.open
	m.facets.open = true
	m.facets.returnMode = m.mode
	m.mode = modeFacetFocus
	m.applySizes()
	if wasOpen {
		return m, nil
	}
	return m.triggerFacetFetch()
}

// closeFacets closes the facet panel outright and returns to whichever
// mode was active before it was opened.
func (m appModel) closeFacets() (appModel, tea.Cmd) {
	m.facets.open = false
	m.mode = m.facets.returnMode
	m.applySizes()
	return m, nil
}

// facetSourceIsTail reports whether the facet panel's data source is the
// live tail buffer (tailModel.entries) rather than a fresh browse-mode
// Client.Facets fetch — true both while actively tailing and while
// focused on a panel that was opened from tail mode (facets.returnMode).
func (m appModel) facetSourceIsTail() bool {
	if m.mode == modeTail {
		return true
	}
	return m.mode == modeFacetFocus && m.facets.returnMode == modeTail
}

// triggerFacetFetch (re)computes the facet panel's counts for whichever
// source is currently active: a synchronous aggregate over the tail
// buffer (facetSourceIsTail), or an async, paginated Client.Facets fetch
// across the browse filter's full time range (see its doc comment for the
// bounded-concurrency, rate-limited, capped-per-range strategy). The
// last-good counts stay visible while a browse-mode fetch is in flight
// (facetModel.setLoading), so reopening/refreshing doesn't flash the
// panel blank.
func (m appModel) triggerFacetFetch() (appModel, tea.Cmd) {
	if m.facetSourceIsTail() {
		m.facets.setResult(gcplog.BuildFacets(m.tail.entries), false)
		return m, nil
	}
	m.facets.setLoading()
	m.facetGen++
	return m, m.fetchFacetsCmd(m.facetGen)
}

// fetchFacetsCmd returns a Cmd that computes facet counts across the
// current filter's full time range via Client.Facets.
func (m appModel) fetchFacetsCmd(gen int) tea.Cmd {
	client, filter := m.client, m.filter
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), facetsTimeout)
		defer cancel()
		// Since/Until must both be resolved for Facets (see its doc
		// comment), the same transient-resolution discipline
		// fetchHistogramCmd already follows.
		f := filter
		if f.Since.IsZero() {
			f.Since = time.Now().Add(-defaultLookback)
		}
		if f.Until.IsZero() {
			f.Until = time.Now()
		}
		result, err := client.Facets(ctx, f)
		return facetsLoadedMsg{gen: gen, result: result, err: err}
	}
}

func (m appModel) handleFacetsLoaded(msg facetsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.facetGen {
		return m, nil // superseded by a newer filter change or panel close/reopen
	}
	if msg.err != nil {
		m.facets.setErr(msg.err)
		return m, nil
	}
	m.facets.setResult(msg.result.Facets, msg.result.Capped)
	return m, nil
}

// applyFacetFilter narrows the active filter to the selected facet value
// and re-runs the query — the facet panel's equivalent of
// filterSubmittedMsg. Tail has no bounded result set to re-fetch, so it
// restarts the stream (via startTail) instead of triggering a browse-mode
// fetch; facets.returnMode (recorded when the panel was opened) says which
// case this is.
func (m appModel) applyFacetFilter(row facetRow) (tea.Model, tea.Cmd) {
	switch row.field {
	case facetFieldSeverity:
		m.filter.ExactSeverity, m.filter.MinSeverity = row.severity, 0
	case facetFieldLogName:
		m.filter.LogName = row.value
	case facetFieldResource:
		m.filter.ResourceType = row.value
	case facetFieldLabel:
		if m.filter.Labels == nil {
			m.filter.Labels = map[string]string{}
		}
		m.filter.Labels[row.labelKey] = row.value
	}
	m.notice = ""
	if m.facets.returnMode == modeTail {
		m.mode = modeTail
		return m.startTail()
	}
	m.mode = modeBrowse
	return m.triggerFetchAndHistogram("", false)
}

// applyRawQuery installs raw as the active RawQuery, clears the mutually
// exclusive structured fields, and refetches from page 1. If Since is
// currently zero (e.g. right after a bare :query with no filter bar ever
// used), it's resolved to defaultLookback here — RawQuery does not exempt
// a FilterState from the Since invariant documented on FilterState.Since,
// any more than the structured fields do.
func (m appModel) applyRawQuery(raw string) (appModel, tea.Cmd) {
	m.filter.RawQuery = strings.TrimSpace(raw)
	m.filter.MinSeverity = 0
	m.filter.ExactSeverity = 0
	m.filter.LogName = ""
	m.filter.ResourceType = ""
	m.filter.FreeText = ""
	m.filter.Labels = nil
	if m.filter.Since.IsZero() {
		m.filter.Since = time.Now().Add(-defaultLookback)
	}
	m.notice = ""
	m.mode = modeBrowse
	return m.triggerFetchAndHistogram("", false)
}

// triggerFetch marks a fetch as in flight (so both listModel's own
// "loading…" empty-state text and the header's spinner reflect it) and
// returns the batched fetch + spinner-tick Cmd. Every fetchPage call that
// isn't the lazy near-bottom prefetch (which sets list.loading itself,
// since it's list_model.Update that decides to trigger it) should go
// through this rather than calling fetchPage directly, or there's no
// visible sign a refresh/filter-submit/project-switch is even happening.
func (m appModel) triggerFetch(pageToken string, appending bool) (appModel, tea.Cmd) {
	m.list.loading = true
	return m, tea.Batch(m.fetchPage(pageToken, appending), m.spinner.Tick)
}

// triggerFetchAndHistogram is triggerFetch plus a histogram recompute — for
// call sites where the filter/time-range itself changed (filterSubmittedMsg,
// applyRawQuery, handleProjectSwitched, the Refresh key, and
// histogramRangeSelectedMsg), as opposed to plain pagination or the lazy
// near-bottom prefetch, which don't move the time window and so leave the
// histogram alone (triggerFetch, not this). The facet panel, if open,
// piggybacks on the same call sites (via triggerFacetFetch) — every place
// that already refreshes the list on a filter change refreshes facets for
// free too, with no separate call sites to remember.
func (m appModel) triggerFetchAndHistogram(pageToken string, appending bool) (appModel, tea.Cmd) {
	m, fetchCmd := m.triggerFetch(pageToken, appending)
	m.histogram.setLoading()
	cmds := []tea.Cmd{fetchCmd, m.fetchHistogramCmd()}
	if m.facets.open {
		var facetCmd tea.Cmd
		m, facetCmd = m.triggerFacetFetch()
		cmds = append(cmds, facetCmd)
	}
	return m, tea.Batch(cmds...)
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

// fetchHistogramCmd returns a Cmd that computes histogram counts across the
// current filter's range, requesting one bucket per terminal column so a
// screen x-coordinate maps 1:1 to a bucket (see histogramColumnAt).
func (m appModel) fetchHistogramCmd() tea.Cmd {
	client, filter, buckets := m.client, m.filter, histogramBucketCount(m.width)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), histogramTimeout)
		defer cancel()
		// Since/Until must both be resolved for Histogram (see its doc
		// comment) even when filter itself legitimately leaves Until zero
		// (unbounded — e.g. live tail). Resolved here, once, transiently,
		// and never written back into filter itself — the same discipline
		// applyRawQuery already follows for Since.
		f := filter
		if f.Since.IsZero() {
			f.Since = time.Now().Add(-defaultLookback)
		}
		if f.Until.IsZero() {
			f.Until = time.Now()
		}
		result, err := client.Histogram(ctx, f, buckets)
		return histogramLoadedMsg{result: result, forBuckets: buckets, err: err}
	}
}

// switchProjectCmd returns a Cmd that opens a new gcplog.Client for
// project, driven from the ":project <id>" command.
//
// Note: this gives the new client a fresh read-rate-limiter budget (see
// gcplog.NewClient), so rapid repeated ":project" switches — including
// switching back to a project just switched away from — can momentarily
// exceed the real GCP-side quota's rolling window even though each
// individual client instance paces itself correctly. Accepted as a rare,
// low-frequency edge case: a project switch is a deliberate, user-initiated
// action, not the sustained per-session usage pattern the limiter is
// designed to keep under quota.
func (m appModel) switchProjectCmd(project string) tea.Cmd {
	quota := m.readQuota
	if quota <= 0 {
		quota = config.DefaultReadQuota
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		client, err := gcplog.NewClient(ctx, project, quota)
		return projectSwitchedMsg{client: client, project: project, err: err}
	}
}

// browseBody joins the log list with the facet side panel when it's open —
// the codebase's first horizontal composition (see facetPanelStyle's doc
// comment in styles.go for why it's also the first bordered element).
func (m appModel) browseBody() string {
	if !m.facets.open {
		return m.list.View()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), facetPanelStyle.Render(m.facets.View()))
}

// tailBody is browseBody's tail-mode analog.
func (m appModel) tailBody() string {
	if !m.facets.open {
		return m.tail.View()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.tail.View(), facetPanelStyle.Render(m.facets.View()))
}

func (m appModel) View() tea.View {
	var body string
	switch m.mode {
	case modeError:
		body = errorStyle.Render("error: " + friendlyError(m.err))
	case modeHelp:
		body = helpText()
	case modeFilterFocus, modeCommand, modeFacetFocus:
		body = m.browseBody()
	case modeDetail:
		body = m.detail.View()
	case modeTail:
		body = m.tailBody()
	case modeQuery:
		body = m.query.View()
	case modeQueries:
		body = m.queriesListText()
	default:
		body = m.browseBody()
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

	histo := ""
	if m.showsHistogram() {
		histo = m.histogram.View() + "\n"
	}

	content := m.renderHeader() + "\n" + histo + body + "\n" + footer
	v := tea.NewView(content)
	v.AltScreen = true
	if m.showsHistogram() {
		// Only in modes that actually use it: other modes (detail, query,
		// help) keep the terminal's native mouse-driven text selection/copy
		// working instead of tailspin capturing every click.
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}
