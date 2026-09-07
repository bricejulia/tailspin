package tui

import (
	"math"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// histogramLines is how many physical lines histogramModel.View always
// renders, regardless of content — appModel's layout budget
// (browseContentHeight) must match this exactly, the same discipline
// headerLines already follows for the header.
const histogramLines = histogramBarRows + 2 // bar rows + a baseline rule + an axis/status line

// histogramBarRows is the vertical resolution of the stacked bar chart.
const histogramBarRows = 4

// minHistogramWidth is the narrowest terminal width the histogram bothers
// rendering at — below it there's no room for a meaningful chart, so it's
// hidden entirely (see appModel.showsHistogram) rather than squeezed.
const minHistogramWidth = 20

// histogramColumn is one bar: one time bucket's counts, broken down by
// severity tier. The number of buckets fetched is capped well below a wide
// terminal's column count (see gcplog.MaxHistogramBuckets — request volume,
// not display resolution, is the limiting factor), so a bucket often spans
// more than one screen column; bucketForColumn maps a screen x-coordinate to
// the bucket rendered there.
type histogramColumn struct {
	start, end time.Time
	counts     [gcplog.NumSeverityTiers]int64
	total      int64
	capped     bool
}

// histogramModel renders the stacked, severity-colored volume chart above
// the log view, and turns a click or drag across its bars into a
// histogramRangeSelectedMsg. It owns no GCP client — appModel triggers the
// Histogram fetch and feeds the result in via setResult, the same
// fetch-lives-in-appModel pattern listModel and tailModel already follow.
type histogramModel struct {
	columns  []histogramColumn
	maxTotal int64 // tallest column's total, used to scale bar heights

	width, height int

	loading bool
	err     error // non-fatal: rendered inline, never takes over modeError

	// fetchedForBuckets is the bucket count the last successful or in-flight
	// fetch was requested with (see histogramBucketCount), so a resize that
	// doesn't actually change that count doesn't trigger a redundant
	// refetch — the common case on any terminal wider than
	// gcplog.MaxHistogramBuckets columns, where the count is pinned at the
	// cap regardless of width.
	fetchedForBuckets int

	// Drag state, active from mouse-down to mouse-up. dragStartCol is fixed
	// at mouse-down; dragCol tracks the most recent column under the
	// cursor, and equals dragStartCol for a plain click (no movement).
	dragging     bool
	dragStartCol int
	dragCol      int

	// selectedFrom/selectedTo mirror the FilterState.Since/Until last
	// applied via a completed drag on this chart (zero if the active
	// filter didn't come from one), so the chart can underline "this is
	// what you're viewing" after the drag completes, not just during it.
	selectedFrom, selectedTo time.Time
}

func newHistogramModel() histogramModel {
	return histogramModel{}
}

// SetSize records the chart's terminal width/height. Rendering itself
// doesn't depend on height (histogramLines is fixed), but the field is kept
// for symmetry with every other sub-model's SetSize and in case a future
// layout ever needs it.
func (m *histogramModel) SetSize(width, height int) {
	m.width, m.height = width, height
}

// setLoading marks a fetch as in flight — histogramModel's own "loading
// histogram…" empty-state text reflects it, same as listModel.loading.
func (m *histogramModel) setLoading() {
	m.loading = true
	m.err = nil
}

// setErr records a failed histogram fetch. Kept separate from appModel.err:
// a histogram is a secondary view over data the log list already shows
// successfully, so its failure is rendered inline rather than promoted to
// modeError, which would take over the whole screen for what's ultimately a
// non-essential chart.
func (m *histogramModel) setErr(err error) {
	m.err = err
	m.loading = false
}

// setResult installs a freshly-fetched HistogramResult, requested for
// forBuckets buckets (recorded so a later resize can tell whether a refetch
// is actually needed).
func (m *histogramModel) setResult(result gcplog.HistogramResult, forBuckets int) {
	m.columns = make([]histogramColumn, len(result.Buckets))
	m.maxTotal = 0
	for i, b := range result.Buckets {
		total := b.Total()
		m.columns[i] = histogramColumn{start: b.Start, end: b.End, counts: b.Counts, total: total, capped: b.Capped}
		if total > m.maxTotal {
			m.maxTotal = total
		}
	}
	m.loading = false
	m.err = nil
	m.fetchedForBuckets = forBuckets
	// A freshly-loaded range always spans the whole chart, so a carried-over
	// selectedFrom/selectedTo from an earlier drag would just underline
	// every column — true but useless. Clearing it here means the
	// underline only ever shows for the (usefully partial) span of a drag
	// that hasn't triggered its own refetch yet.
	m.selectedFrom, m.selectedTo = time.Time{}, time.Time{}
}

// reset clears the chart back to empty (a fresh query whose result hasn't
// landed yet) while keeping its current size.
func (m *histogramModel) reset() {
	m.columns = nil
	m.maxTotal = 0
	m.err = nil
	m.selectedFrom, m.selectedTo = time.Time{}, time.Time{}
	m.dragging = false
}

// beginDrag starts a click/drag gesture at col (a column index, from
// appModel.histogramColumnAt).
func (m *histogramModel) beginDrag(col int) {
	if len(m.columns) == 0 {
		return
	}
	m.dragging = true
	m.dragStartCol = col
	m.dragCol = col
}

// updateDrag tracks the cursor moving to col while a drag is in progress; a
// no-op if no drag is active (e.g. the mouse-down happened outside the
// chart).
func (m *histogramModel) updateDrag(col int) {
	if !m.dragging {
		return
	}
	m.dragCol = col
}

// bucketForColumn maps screen x-coordinate x (0-indexed, within width) to a
// bucket index in a bucketCount-length slice — used both to decide which
// bucket a screen column renders (rendering iterates x, looks up the
// bucket) and to hit-test a mouse event's x into a bucket (appModel calls
// this directly). bucketCount is usually smaller than width (see
// gcplog.MaxHistogramBuckets), so this is a many-screen-columns-to-one-
// bucket mapping, not 1:1.
func bucketForColumn(x, width, bucketCount int) int {
	if bucketCount <= 0 || width <= 0 {
		return 0
	}
	i := x * bucketCount / width
	return min(max(i, 0), bucketCount-1)
}

// endDrag completes a click/drag gesture at col, and — if the chart has
// data — returns a Cmd emitting histogramRangeSelectedMsg for the spanned
// time range. A same-column click/release (no movement) is a valid
// single-bucket selection, not a special case: terminal mouse events are
// already cell-granular, so there's no jitter to threshold against.
func (m *histogramModel) endDrag(col int) tea.Cmd {
	m.dragging = false
	if len(m.columns) == 0 {
		return nil
	}
	lo, hi := m.dragStartCol, col
	if lo > hi {
		lo, hi = hi, lo
	}
	lo = min(max(lo, 0), len(m.columns)-1)
	hi = min(max(hi, 0), len(m.columns)-1)

	since, until := m.columns[lo].start, m.columns[hi].end
	m.selectedFrom, m.selectedTo = since, until
	return func() tea.Msg { return histogramRangeSelectedMsg{since: since, until: until} }
}

// cancelDrag discards an in-progress drag without emitting a selection —
// used when a resize invalidates the column boundaries a drag in progress
// was tracking.
func (m *histogramModel) cancelDrag() {
	m.dragging = false
}

// stackOrder is bottom-to-top: the least severe band forms each bar's base,
// the most severe is the segment nearest the top — matching GCP's own
// histogram convention, so the most severe segment is the first thing the
// eye picks out scanning across many bars.
var stackOrder = [gcplog.NumSeverityTiers]gcplog.SeverityTier{
	gcplog.SeverityTierDefault,
	gcplog.SeverityTierInfo,
	gcplog.SeverityTierWarning,
	gcplog.SeverityTierError,
	gcplog.SeverityTierAlert,
}

// barSegmentRows distributes totalRows cells across counts' severity tiers
// proportionally, using largest-remainder rounding so the rows actually
// stacked sum to exactly totalRows (a naive per-tier round can under- or
// over-shoot by a cell or two).
func barSegmentRows(counts [gcplog.NumSeverityTiers]int64, total int64, totalRows int) [gcplog.NumSeverityTiers]int {
	var rows [gcplog.NumSeverityTiers]int
	if total <= 0 || totalRows <= 0 {
		return rows
	}

	var remainder [gcplog.NumSeverityTiers]float64
	assigned := 0
	for t, c := range counts {
		if c == 0 {
			continue
		}
		exact := float64(c) / float64(total) * float64(totalRows)
		rows[t] = int(exact)
		remainder[t] = exact - float64(rows[t])
		assigned += rows[t]
	}
	for assigned < totalRows {
		best := -1
		for t, c := range counts {
			if c == 0 {
				continue
			}
			if best == -1 || remainder[t] > remainder[best] {
				best = t
			}
		}
		if best == -1 {
			break
		}
		rows[best]++
		remainder[best] = -1 // consumed, don't pick it again
		assigned++
	}
	return rows
}

// tierAtRow reports which severity tier (per stackOrder) occupies row r
// (0-indexed from the bottom of the bar) given that tier's segment heights,
// or false if the bar doesn't reach that high.
func tierAtRow(segRows [gcplog.NumSeverityTiers]int, r int) (gcplog.SeverityTier, bool) {
	cum := 0
	for _, tier := range stackOrder {
		h := segRows[tier]
		if h == 0 {
			continue
		}
		if r >= cum && r < cum+h {
			return tier, true
		}
		cum += h
	}
	return 0, false
}

// inDragSpan reports whether column i falls within the currently in-progress
// drag's span (inclusive) — used to overlay a reverse-video highlight while
// dragging.
func (m histogramModel) inDragSpan(i int) bool {
	if !m.dragging {
		return false
	}
	lo, hi := m.dragStartCol, m.dragCol
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

// inSelectedSpan reports whether column i's time range overlaps the last
// completed drag selection, if any — used to underline "this is what you're
// currently viewing" on the baseline row after a drag completes.
func (m histogramModel) inSelectedSpan(col histogramColumn) bool {
	if m.selectedFrom.IsZero() && m.selectedTo.IsZero() {
		return false
	}
	return col.end.After(m.selectedFrom) && col.start.Before(m.selectedTo)
}

// View renders the chart as exactly histogramLines lines, regardless of
// loading/error/empty/populated state — see histogramLines' doc comment for
// why that budget must never vary.
func (m histogramModel) View() string {
	if m.width < minHistogramWidth {
		return strings.Repeat("\n", histogramLines-1)
	}

	var barLines []string
	var statusLine string

	switch {
	case m.err != nil:
		barLines = m.blankBarRows()
		statusLine = statusStyle.Render(truncate(strings.Join(strings.Fields(m.err.Error()), " "), m.width))
	case len(m.columns) == 0 && m.loading:
		barLines = m.blankBarRows()
		statusLine = statusStyle.Render("loading histogram…")
	case len(m.columns) == 0:
		barLines = m.blankBarRows()
		statusLine = statusStyle.Render("no log entries in range")
	default:
		barLines = m.barRowsView()
		statusLine = m.axisLine()
	}

	lines := make([]string, 0, histogramLines)
	lines = append(lines, barLines...)
	lines = append(lines, m.baselineRow())
	lines = append(lines, statusLine)
	for len(lines) < histogramLines {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m histogramModel) blankBarRows() []string {
	lines := make([]string, histogramBarRows)
	for i := range lines {
		lines[i] = ""
	}
	return lines
}

// baselineRow draws the seam between the bars and the axis/status line —
// underlined wherever the last completed drag selection's range overlaps,
// so the currently-viewed range stays visible after the drag ends.
func (m histogramModel) baselineRow() string {
	if len(m.columns) == 0 {
		return strings.Repeat("─", m.width)
	}
	var out strings.Builder
	for x := range m.width {
		c := m.columns[bucketForColumn(x, m.width, len(m.columns))]
		style := lipgloss.NewStyle()
		if m.inSelectedSpan(c) {
			style = style.Foreground(colorAccent).Bold(true)
		}
		out.WriteString(style.Render("─"))
	}
	return out.String()
}

// barRowsView renders histogramBarRows lines, top row first. Each bucket's
// segment heights are computed once and then looked up per screen column
// (see bucketForColumn), since a bucket commonly spans more than one column.
func (m histogramModel) barRowsView() []string {
	segs := make([][gcplog.NumSeverityTiers]int, len(m.columns))
	for i, c := range m.columns {
		totalRows := 0
		if m.maxTotal > 0 && c.total > 0 {
			totalRows = int(math.Round(float64(c.total) / float64(m.maxTotal) * float64(histogramBarRows)))
			if totalRows == 0 {
				totalRows = 1 // any nonzero bucket stays visible
			}
		}
		segs[i] = barSegmentRows(c.counts, c.total, totalRows)
	}

	lines := make([]string, histogramBarRows)
	for r := histogramBarRows - 1; r >= 0; r-- {
		var out strings.Builder
		for x := range m.width {
			bi := bucketForColumn(x, m.width, len(m.columns))
			cell := " "
			style := lipgloss.NewStyle()
			if tier, ok := tierAtRow(segs[bi], r); ok {
				cell = "█"
				style = style.Foreground(tierColor[tier])
			}
			if m.inDragSpan(bi) {
				style = style.Reverse(true)
			}
			out.WriteString(style.Render(cell))
		}
		lines[histogramBarRows-1-r] = out.String()
	}
	return lines
}

// axisLine shows the loaded range's start (left) and end (right), plus a
// muted peak marker so there's a sense of scale without spending a column on
// a numeric y-axis. A "~" prefix on the peak marker flags that one or more
// buckets hit their entry cap or a per-bucket query failure (see
// gcplog.HistogramBucket.Capped) — the counts shown are then a lower bound,
// not exact.
func (m histogramModel) axisLine() string {
	start, end := m.columns[0].start.Local(), m.columns[len(m.columns)-1].end.Local()
	left := formatAxisTime(start, end)
	right := formatAxisTime(end, start)

	var mid string
	switch {
	case m.maxTotal > 0 && m.anyCapped():
		mid = "~peak " + formatCount(m.maxTotal) + "/bucket  "
	case m.maxTotal > 0:
		mid = "peak " + formatCount(m.maxTotal) + "/bucket  "
	case m.anyCapped():
		// Every bucket capped with nothing tallied at all (a read-quota
		// budget too tight to make progress before the fetch's timeout,
		// most likely) looks identical to a genuinely empty range unless
		// called out explicitly — maxTotal alone can't distinguish them.
		mid = "data incomplete  "
	}
	return padBetween(statusStyle.Render(left), statusStyle.Render(mid+right), m.width)
}

// formatAxisTime renders t for the axis line: time-only ("15:04") when it
// falls on the same calendar day as other (the range's other endpoint),
// otherwise with the date too ("Jan 2 15:04") — plain time-of-day is
// ambiguous once the histogram spans more than a day (e.g. the filter
// bar's 7d/30d "since" presets, or a 24h window that happens to cross
// midnight), so the date is only added when it's actually needed to tell
// the start and end apart.
func formatAxisTime(t, other time.Time) string {
	if sameDate(t, other) {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

func sameDate(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// anyCapped reports whether any loaded bucket's counts are a known lower
// bound rather than exact (see gcplog.HistogramBucket.Capped).
func (m histogramModel) anyCapped() bool {
	for _, c := range m.columns {
		if c.capped {
			return true
		}
	}
	return false
}

// formatCount renders n compactly (e.g. "1.2k") once it's large enough that
// full digits would crowd the axis line — the histogram's own numbers can
// run much higher than anything else in the UI (a single bucket can hold
// tens of thousands of entries), unlike the header's entry/page counts.
func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return trimCountSuffix(float64(n)/1_000_000) + "m"
	case n >= 1_000:
		return trimCountSuffix(float64(n)/1_000) + "k"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// trimCountSuffix renders f with one decimal place, dropping a trailing
// ".0" ("1.0" -> "1") so whole numbers don't carry a meaningless decimal.
func trimCountSuffix(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}
