package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// facetFieldKind identifies which of Entry's faceted fields a facetRow
// belongs to.
type facetFieldKind int

const (
	facetFieldSeverity facetFieldKind = iota
	facetFieldLogName
	facetFieldResource
	facetFieldLabel
)

// facetRow is one selectable value line in the facet panel: a field, its
// value, and how many loaded entries carry it. severity carries the actual
// Severity (not just its rendered string) so applying it as a filter never
// has to lossily parse a formatted label back into an enum; labelKey is
// only set when field is facetFieldLabel, since that's the only field with
// more than one possible "column" of values.
type facetRow struct {
	field    facetFieldKind
	labelKey string
	value    string
	severity gcplog.Severity
	count    int
}

// facetLine is one physical row in the panel's flattened render/selection
// order: either a field header (not selectable) or a facetRow.
type facetLine struct {
	isHeader bool
	header   string
	row      facetRow
}

// facetValueSelectedMsg is emitted when the user presses enter on a facet
// value — the facet panel's equivalent of filterSubmittedMsg.
type facetValueSelectedMsg struct {
	row facetRow
}

// facetModel renders the toggleable facet side panel: a field-by-field
// breakdown of the current result set (see gcplog.Facets), navigable with
// j/k and applied as a filter with enter. It owns no GCP client — appModel
// triggers the Client.Facets fetch (browse mode) or the synchronous
// gcplog.BuildFacets aggregate (tail mode) and feeds the result in via
// setResult, the same fetch-lives-in-appModel pattern every other
// sub-model follows.
type facetModel struct {
	// open is whether the panel is visible at all; returnMode is which
	// mode to fall back to when it's closed or unfocused — the panel can
	// be open-but-unfocused (browsing/tailing normally, panel still shown)
	// or open-and-focused (mode == modeFacetFocus, keys navigate it).
	open       bool
	returnMode mode

	loading, truncated bool
	err                error

	facets gcplog.Facets
	rows   []facetLine
	// lineOf[i] is the physical viewport line facetLine i rendered at,
	// kept in step with rows by refresh() — used to scroll the selected
	// row into view without re-deriving line positions from scratch.
	lineOf   []int
	selected int

	viewport      viewport.Model
	width, height int
}

func newFacetModel() facetModel {
	return facetModel{viewport: viewport.New()}
}

func (m *facetModel) SetSize(width, height int) {
	m.width, m.height = width, height
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	m.refresh()
}

// reset clears the panel back to empty (a fresh session whose counts
// haven't landed yet, or don't apply anymore — a project switch, a new
// tail session) while keeping its open/returnMode state.
func (m *facetModel) reset() {
	m.facets = gcplog.Facets{}
	m.rows = nil
	m.lineOf = nil
	m.selected = 0
	m.loading = false
	m.truncated = false
	m.err = nil
	m.refresh()
}

// setLoading marks a fetch as in flight. The last-good rows are left in
// place (see refresh's status line) so reopening/refreshing the panel
// doesn't flash it blank while the fetch is running.
func (m *facetModel) setLoading() {
	m.loading = true
	m.err = nil
	m.refresh()
}

// setErr records a failed fetch. Kept separate from appModel.err: like the
// histogram, the facet panel is a secondary view over data the log list
// already shows successfully, so its failure renders inline rather than
// taking over the whole screen.
func (m *facetModel) setErr(err error) {
	m.err = err
	m.loading = false
	m.refresh()
}

// setResult installs a freshly-computed Facets breakdown, truncated per
// FacetsResult.Capped (always false for the tail-mode source, which never
// caps).
func (m *facetModel) setResult(facets gcplog.Facets, truncated bool) {
	m.facets = facets
	m.truncated = truncated
	m.loading = false
	m.err = nil
	m.buildRows()
}

// buildRows flattens facets into rows: one header line per non-empty
// field, followed by that field's values. selected is reset to the first
// selectable line if it fell out of range (e.g. a field that had values
// last time no longer does).
func (m *facetModel) buildRows() {
	var rows []facetLine

	if len(m.facets.Severity) > 0 {
		rows = append(rows, facetLine{isHeader: true, header: "Severity"})
		for _, c := range m.facets.Severity {
			rows = append(rows, facetLine{row: facetRow{field: facetFieldSeverity, severity: c.Severity, count: c.Count}})
		}
	}

	addField := func(header string, counts []gcplog.FacetCount, field facetFieldKind, labelKey string) {
		if len(counts) == 0 {
			return
		}
		rows = append(rows, facetLine{isHeader: true, header: header})
		for _, c := range counts {
			rows = append(rows, facetLine{row: facetRow{field: field, labelKey: labelKey, value: c.Value, count: c.Count}})
		}
	}
	addField("Log Name", m.facets.LogName, facetFieldLogName, "")
	addField("Resource", m.facets.Resource, facetFieldResource, "")
	for _, lf := range m.facets.Labels {
		addField("Labels: "+lf.Key, lf.Values, facetFieldLabel, lf.Key)
	}

	m.rows = rows
	if m.selected < 0 || m.selected >= len(m.rows) || m.rows[m.selected].isHeader {
		m.selected = m.firstSelectable()
	}
	m.refresh()
}

func (m facetModel) firstSelectable() int {
	for i, ln := range m.rows {
		if !ln.isHeader {
			return i
		}
	}
	return 0
}

// Update handles panel navigation: j/k move the selection between
// selectable (non-header) rows, pgup/pgdn scroll the viewport, and enter
// emits facetValueSelectedMsg for the selected row. esc/the panel toggle
// key are handled at the app level (handleKey's modeFacetFocus case), the
// same split modeFilterFocus's filterBarModel uses.
func (m facetModel) Update(msg tea.Msg) (facetModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(keyMsg, keys.Down):
		m.moveSelection(1)
	case key.Matches(keyMsg, keys.Up):
		m.moveSelection(-1)
	case key.Matches(keyMsg, keys.PageDown):
		m.viewport.PageDown()
	case key.Matches(keyMsg, keys.PageUp):
		m.viewport.PageUp()
	case keyMsg.String() == "enter":
		if m.selected >= 0 && m.selected < len(m.rows) && !m.rows[m.selected].isHeader {
			row := m.rows[m.selected].row
			return m, func() tea.Msg { return facetValueSelectedMsg{row: row} }
		}
	}
	return m, nil
}

// moveSelection steps the selection by delta (±1, from a single Up/Down
// press), skipping over header lines, and clamps at either end rather than
// wrapping — the same clamp convention listModel.moveSelection uses.
// Clamping means a run of steps that hits the edge while still sitting on
// a header (nothing selectable further in that direction — e.g. stepping
// up from the very first value row, which only has its own field's header
// above it) must leave the selection exactly where it started, not settle
// on that header.
func (m *facetModel) moveSelection(delta int) {
	if len(m.rows) == 0 {
		return
	}
	next := m.selected
	for {
		cand := next + delta
		if cand < 0 || cand >= len(m.rows) {
			break // can't step further this way — next is the last position tried
		}
		next = cand
		if !m.rows[next].isHeader {
			break // landed on a selectable row
		}
	}
	if next == m.selected || m.rows[next].isHeader {
		return // no legal move: blocked immediately, or only headers that way
	}
	m.selected = next
	m.refresh()
	m.scrollSelectedIntoView()
}

// scrollSelectedIntoView scrolls the viewport by the minimum amount needed
// to bring the selected row's line into view — listModel.scrollIntoView's
// single-line-row analog (every facet row is exactly one physical line, so
// there's no group span to track).
func (m *facetModel) scrollSelectedIntoView() {
	if m.selected < 0 || m.selected >= len(m.lineOf) {
		return
	}
	line := m.lineOf[m.selected]
	switch {
	case line < m.viewport.YOffset():
		m.viewport.SetYOffset(line)
	case line >= m.viewport.YOffset()+m.height:
		m.viewport.SetYOffset(line - m.height + 1)
	}
}

// refresh re-renders the panel's content into the viewport from its
// current state (status line, rows, selection) — called after every
// mutation, the same discipline listModel.render follows.
func (m *facetModel) refresh() {
	var lines []string
	m.lineOf = make([]int, len(m.rows))

	if s := m.statusLine(); s != "" {
		lines = append(lines, s, "")
	}
	for i, ln := range m.rows {
		if ln.isHeader {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			m.lineOf[i] = len(lines)
			lines = append(lines, detailLabelStyle.Render(ln.header))
			continue
		}
		m.lineOf[i] = len(lines)
		lines = append(lines, m.renderRow(i, ln.row))
	}
	m.viewport.SetContentLines(lines)
}

func (m facetModel) statusLine() string {
	switch {
	case m.err != nil:
		return errorStyle.Render(truncate(strings.Join(strings.Fields(m.err.Error()), " "), max(m.width, 1)))
	case m.loading && len(m.rows) == 0:
		return statusStyle.Render("loading facets…")
	case m.loading:
		return statusStyle.Render("refreshing facets…")
	case len(m.rows) == 0:
		return statusStyle.Render("no facets for the current range")
	case m.truncated:
		return warningStyle.Render("partial scan — narrow time range for exact counts")
	default:
		return ""
	}
}

// renderRow renders one value line as "value  count", value colored by
// severity tier for facetFieldSeverity rows (reusing tierColor, the same
// source list rows/detail/histogram all get severity color from) and
// plain otherwise. The selected row is rendered as plain, unstyled text
// with exactly one style applied over the whole line — entryPrefixPlain's
// approach in list_model.go, and for the same reason: a separately
// Render()'d, self-resetting per-column style would cut the selection
// background short partway through the line.
func (m facetModel) renderRow(i int, row facetRow) string {
	value := row.value
	if row.field == facetFieldSeverity {
		value = strings.ToUpper(row.severity.String())
	}
	countStr := strconv.Itoa(row.count)
	valueWidth := max(m.width-len(countStr)-2, 4)
	valuePlain := padToWidth(truncate(value, valueWidth), valueWidth)

	if i == m.selected {
		return selectedRowStyle.Render(padToWidth(valuePlain+"  "+countStr, m.width))
	}
	if row.field == facetFieldSeverity {
		style := plainStyle.Foreground(tierColor[gcplog.ClassifySeverity(row.severity)])
		return style.Render(valuePlain) + "  " + statusStyle.Render(countStr)
	}
	return plainStyle.Render(valuePlain) + "  " + statusStyle.Render(countStr)
}

func (m facetModel) View() string {
	return m.viewport.View()
}
