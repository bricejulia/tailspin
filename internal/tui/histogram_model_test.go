package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

func TestBarSegmentRowsSumsExactly(t *testing.T) {
	// Largest-remainder rounding must always sum to exactly totalRows, even
	// when the per-tier proportions don't divide evenly.
	counts := [gcplog.NumSeverityTiers]int64{
		gcplog.SeverityTierDefault: 7,
		gcplog.SeverityTierInfo:    3,
		gcplog.SeverityTierError:   1,
	}
	total := int64(11)

	for totalRows := 0; totalRows <= histogramBarRows; totalRows++ {
		rows := barSegmentRows(counts, total, totalRows)
		sum := 0
		for _, r := range rows {
			sum += r
		}
		if sum != totalRows {
			t.Errorf("totalRows=%d: sum(rows) = %d, want %d (rows=%v)", totalRows, sum, totalRows, rows)
		}
	}
}

func TestBarSegmentRowsZeroTotal(t *testing.T) {
	var counts [gcplog.NumSeverityTiers]int64
	rows := barSegmentRows(counts, 0, histogramBarRows)
	for _, r := range rows {
		if r != 0 {
			t.Errorf("rows = %v, want all zero for an empty bucket", rows)
		}
	}
}

func TestTierAtRowStacksBottomToTop(t *testing.T) {
	// Default(2 rows) at the bottom, then Error(1 row) above it.
	var seg [gcplog.NumSeverityTiers]int
	seg[gcplog.SeverityTierDefault] = 2
	seg[gcplog.SeverityTierError] = 1

	cases := []struct {
		row      int
		wantTier gcplog.SeverityTier
		wantOK   bool
	}{
		{0, gcplog.SeverityTierDefault, true},
		{1, gcplog.SeverityTierDefault, true},
		{2, gcplog.SeverityTierError, true},
		{3, 0, false}, // above the stack
	}
	for _, tc := range cases {
		tier, ok := tierAtRow(seg, tc.row)
		if ok != tc.wantOK || (ok && tier != tc.wantTier) {
			t.Errorf("tierAtRow(seg, %d) = (%v, %v), want (%v, %v)", tc.row, tier, ok, tc.wantTier, tc.wantOK)
		}
	}
}

func TestHistogramModelSetResult(t *testing.T) {
	var m histogramModel
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	result := gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{
		{Start: since, End: since.Add(time.Hour), Counts: [gcplog.NumSeverityTiers]int64{gcplog.SeverityTierDefault: 3}},
		{Start: since.Add(time.Hour), End: since.Add(2 * time.Hour), Counts: [gcplog.NumSeverityTiers]int64{gcplog.SeverityTierError: 10}},
	}}

	m.setResult(result, 2)

	if len(m.columns) != 2 {
		t.Fatalf("len(columns) = %d, want 2", len(m.columns))
	}
	if m.maxTotal != 10 {
		t.Errorf("maxTotal = %d, want 10 (the larger bucket's total)", m.maxTotal)
	}
	if m.fetchedForBuckets != 2 {
		t.Errorf("fetchedForBuckets = %d, want 2", m.fetchedForBuckets)
	}
	if m.loading {
		t.Error("loading should be cleared after setResult")
	}
}

func TestHistogramModelDragProducesRangeSelectedMsg(t *testing.T) {
	var m histogramModel
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	var buckets []gcplog.HistogramBucket
	for i := range 5 {
		buckets = append(buckets, gcplog.HistogramBucket{
			Start: since.Add(time.Duration(i) * time.Hour),
			End:   since.Add(time.Duration(i+1) * time.Hour),
		})
	}
	m.setResult(gcplog.HistogramResult{Buckets: buckets}, 5)

	// Drag from column 3 back to column 1 (reversed) — the selection should
	// still span [col1.start, col3.end), lowest to highest regardless of
	// drag direction.
	m.beginDrag(3)
	m.updateDrag(1)
	cmd := m.endDrag(1)
	if m.dragging {
		t.Error("dragging should be false after endDrag")
	}
	if cmd == nil {
		t.Fatal("endDrag returned a nil Cmd, want a histogramRangeSelectedMsg Cmd")
	}
	msg, ok := cmd().(histogramRangeSelectedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want histogramRangeSelectedMsg", cmd())
	}
	if !msg.since.Equal(buckets[1].Start) {
		t.Errorf("since = %v, want %v", msg.since, buckets[1].Start)
	}
	if !msg.until.Equal(buckets[3].End) {
		t.Errorf("until = %v, want %v", msg.until, buckets[3].End)
	}
}

func TestHistogramModelClickSingleColumn(t *testing.T) {
	// No movement between mouse-down and mouse-up is a valid single-bucket
	// selection, not a special "too small" case.
	var m histogramModel
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	buckets := []gcplog.HistogramBucket{
		{Start: since, End: since.Add(time.Hour)},
		{Start: since.Add(time.Hour), End: since.Add(2 * time.Hour)},
	}
	m.setResult(gcplog.HistogramResult{Buckets: buckets}, 2)

	m.beginDrag(1)
	cmd := m.endDrag(1)
	msg := cmd().(histogramRangeSelectedMsg)
	if !msg.since.Equal(buckets[1].Start) || !msg.until.Equal(buckets[1].End) {
		t.Errorf("single-column click selected [%v,%v), want [%v,%v)", msg.since, msg.until, buckets[1].Start, buckets[1].End)
	}
}

func TestHistogramModelDragOnEmptyChartIsNoop(t *testing.T) {
	var m histogramModel
	m.beginDrag(0)
	if m.dragging {
		t.Error("beginDrag should be a no-op on an empty chart")
	}
	if cmd := m.endDrag(0); cmd != nil {
		t.Error("endDrag on an empty chart should return a nil Cmd")
	}
}

func TestHistogramModelCancelDrag(t *testing.T) {
	var m histogramModel
	m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{{}, {}}}, 2)
	m.beginDrag(0)
	m.updateDrag(1)
	m.cancelDrag()
	if m.dragging {
		t.Error("dragging should be false after cancelDrag")
	}
}

func TestHistogramModelViewAlwaysExactlyHistogramLines(t *testing.T) {
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		setup func(m *histogramModel)
	}{
		{"empty/no fetch yet", func(m *histogramModel) {}},
		{"loading", func(m *histogramModel) { m.setLoading() }},
		{"error", func(m *histogramModel) { m.setErr(errFake) }},
		{"populated", func(m *histogramModel) {
			m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{
				{Start: since, End: since.Add(time.Hour), Counts: [gcplog.NumSeverityTiers]int64{gcplog.SeverityTierWarning: 4}},
			}}, 40)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newHistogramModel()
			m.SetSize(40, histogramLines)
			tc.setup(&m)

			lines := strings.Split(m.View(), "\n")
			if len(lines) != histogramLines {
				t.Errorf("View() produced %d lines, want exactly %d: %q", len(lines), histogramLines, lines)
			}
		})
	}
}

func TestHistogramModelViewHiddenBelowMinWidth(t *testing.T) {
	m := newHistogramModel()
	m.SetSize(minHistogramWidth-1, histogramLines)
	lines := strings.Split(m.View(), "\n")
	if len(lines) != histogramLines {
		t.Errorf("View() produced %d lines below minHistogramWidth, want exactly %d (still occupying the fixed budget)", len(lines), histogramLines)
	}
}

func TestHistogramModelReset(t *testing.T) {
	m := newHistogramModel()
	m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{{}}}, 1)
	m.selectedFrom = time.Now()
	m.dragging = true

	m.reset()

	if len(m.columns) != 0 {
		t.Errorf("columns not cleared after reset: %v", m.columns)
	}
	if m.dragging {
		t.Error("dragging should be cleared after reset")
	}
	if !m.selectedFrom.IsZero() {
		t.Error("selectedFrom should be cleared after reset")
	}
}

func TestBucketForColumn(t *testing.T) {
	cases := []struct {
		x, width, bucketCount int
		want                  int
	}{
		{x: 0, width: 10, bucketCount: 10, want: 0}, // 1:1 mapping
		{x: 9, width: 10, bucketCount: 10, want: 9},
		{x: 0, width: 100, bucketCount: 4, want: 0}, // many columns per bucket
		{x: 24, width: 100, bucketCount: 4, want: 0},
		{x: 25, width: 100, bucketCount: 4, want: 1},
		{x: 99, width: 100, bucketCount: 4, want: 3},  // last column, no overrun
		{x: -5, width: 100, bucketCount: 4, want: 0},  // clamped
		{x: 500, width: 100, bucketCount: 4, want: 3}, // clamped
		{x: 0, width: 10, bucketCount: 0, want: 0},    // no data yet
	}
	for _, tc := range cases {
		if got := bucketForColumn(tc.x, tc.width, tc.bucketCount); got != tc.want {
			t.Errorf("bucketForColumn(%d, %d, %d) = %d, want %d", tc.x, tc.width, tc.bucketCount, got, tc.want)
		}
	}
}

func TestAxisLineReportsIncompleteWhenAllBucketsCappedAndEmpty(t *testing.T) {
	m := newHistogramModel()
	m.SetSize(80, histogramLines)
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	// Every bucket capped with nothing tallied — e.g. a read-quota budget
	// too tight to make any progress before the fetch's timeout — must not
	// silently render identically to a genuinely empty range.
	m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{
		{Start: since, End: since.Add(time.Hour), Capped: true},
		{Start: since.Add(time.Hour), End: since.Add(2 * time.Hour), Capped: true},
	}}, 2)

	if !strings.Contains(m.axisLine(), "incomplete") {
		t.Errorf("axisLine() = %q, want it to flag the data as incomplete", m.axisLine())
	}
}

func TestFormatAxisTime(t *testing.T) {
	noon := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	sameDayLater := time.Date(2026, 9, 3, 18, 30, 0, 0, time.UTC)
	nextDay := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	if got, want := formatAxisTime(noon, sameDayLater), "12:00"; got != want {
		t.Errorf("formatAxisTime (same day as other) = %q, want %q", got, want)
	}
	if got, want := formatAxisTime(noon, nextDay), "Sep 3 12:00"; got != want {
		t.Errorf("formatAxisTime (different day than other) = %q, want %q", got, want)
	}
}

func TestAxisLineShowsDateWhenRangeSpansMultipleDays(t *testing.T) {
	// Plain "15:04" is ambiguous once the histogram spans more than a
	// day (e.g. a 7d/30d "since" preset) — the axis line must include
	// the date in that case, not just the time of day.
	m := newHistogramModel()
	m.SetSize(80, histogramLines)
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	end := start.Add(10 * 24 * time.Hour)
	m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{
		{Start: start, End: end, Counts: [gcplog.NumSeverityTiers]int64{gcplog.SeverityTierInfo: 3}},
	}}, 1)

	if line := m.axisLine(); !strings.Contains(line, "Sep") {
		t.Errorf("axisLine() = %q, want it to include the month/day for a multi-day range", line)
	}
}

func TestAxisLineOmitsDateForSameDayRange(t *testing.T) {
	m := newHistogramModel()
	m.SetSize(80, histogramLines)
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	m.setResult(gcplog.HistogramResult{Buckets: []gcplog.HistogramBucket{
		{Start: start, End: end, Counts: [gcplog.NumSeverityTiers]int64{gcplog.SeverityTierInfo: 3}},
	}}, 1)

	if line := m.axisLine(); strings.Contains(line, "Sep") {
		t.Errorf("axisLine() = %q, want no date clutter for a same-day range", line)
	}
}
