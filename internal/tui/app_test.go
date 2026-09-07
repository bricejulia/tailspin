package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"cloud.google.com/go/logging"

	"github.com/bricejulia/tailspin/internal/config"
	"github.com/bricejulia/tailspin/internal/gcplog"
	"github.com/bricejulia/tailspin/internal/gcplog/gcplogtest"
)

// findMsg synchronously invokes cmd and returns the first message of type T
// it produces, recursing into tea.BatchMsg — Init and several handlers now
// batch a fetch together with the spinner's Tick Cmd (see triggerFetch), so
// the message under test is no longer always cmd()'s direct result.
func findMsg[T any](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	return findInMsg[T](cmd())
}

func findInMsg[T any](msg tea.Msg) (T, bool) {
	var zero T
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if found, ok := findInMsg[T](c()); ok {
				return found, true
			}
		}
		return zero, false
	}
	if typed, ok := msg.(T); ok {
		return typed, true
	}
	return zero, false
}

func textKey(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

func TestInitFetchesFirstPage(t *testing.T) {
	fake := &gcplogtest.Client{
		Pages: []gcplog.Page{{
			Entries: []gcplog.Entry{
				{Timestamp: time.Now(), Severity: logging.Info, Summary: "one"},
				{Timestamp: time.Now(), Severity: logging.Error, Summary: "two"},
			},
		}},
	}

	m := New(fake, "test-project")
	loaded, ok := findMsg[entriesLoadedMsg](m.Init())
	if !ok {
		t.Fatal("Init() cmd produced no entriesLoadedMsg")
	}
	if loaded.err != nil {
		t.Fatalf("unexpected error: %v", loaded.err)
	}

	updated, _ := m.Update(loaded)
	m2 := updated.(appModel)
	if len(m2.list.entries) != 2 {
		t.Fatalf("list has %d entries, want 2", len(m2.list.entries))
	}
	if m2.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m2.mode)
	}
}

func TestInitFetchError(t *testing.T) {
	wantErr := gcplogtest.Client{ListErr: errFake}
	m := New(&wantErr, "test-project")
	loaded, ok := findMsg[entriesLoadedMsg](m.Init())
	if !ok {
		t.Fatal("Init() cmd produced no entriesLoadedMsg")
	}

	updated, _ := m.Update(loaded)
	m2 := updated.(appModel)
	if m2.mode != modeError {
		t.Fatalf("mode = %v, want modeError after a failed fetch", m2.mode)
	}
	if m2.err == nil {
		t.Error("err is nil, want the fetch error to be recorded")
	}
}

func TestFilterKeyOpensFilterBar(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	updated, _ := m.Update(textKey("/"))
	m2 := updated.(appModel)
	if m2.mode != modeFilterFocus {
		t.Errorf("mode = %v, want modeFilterFocus after '/'", m2.mode)
	}
}

func TestCommandKeyOpensCommandMode(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	updated, _ := m.Update(textKey(":"))
	m2 := updated.(appModel)
	if m2.mode != modeCommand {
		t.Errorf("mode = %v, want modeCommand after ':'", m2.mode)
	}
}

func TestQuitKeyReturnsQuitCmd(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	_, cmd := m.Update(textKey("q"))
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for 'q'")
	}
}

func TestTailStaleGenerationIgnored(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.tailGen = 5 // simulate having already moved on from an earlier session

	updated, cmd := m.handleTailStarted(tailStartedMsg{gen: 1, err: nil})
	m2 := updated.(appModel)
	if m2.tailCancel != nil {
		t.Error("a stale tailStartedMsg should not install a cancel func")
	}
	if cmd != nil {
		t.Error("a stale tailStartedMsg should not issue a wait Cmd")
	}
}

func TestQueryCommandOpensQueryModeSeeded(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.filter.RawQuery = `resource.type="k8s_container"`

	updated, _ := m.Update(commandSubmittedMsg{cmd: parsedCommand{Kind: cmdQuery}})
	m2 := updated.(appModel)
	if m2.mode != modeQuery {
		t.Errorf("mode = %v, want modeQuery", m2.mode)
	}
	if got := m2.query.textarea.Value(); got != m.filter.RawQuery {
		t.Errorf("query editor seeded with %q, want %q", got, m.filter.RawQuery)
	}
}

func TestQuerySubmittedAppliesRawQueryAndFetches(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.filter.MinSeverity = logging.Error
	m.filter.LogName = "syslog"

	rawQuery := "resource.type=\"k8s_container\"\nresource.labels.cluster_name=\"my-cluster\""
	updated, cmd := m.Update(querySubmittedMsg{rawQuery: rawQuery})
	m2 := updated.(appModel)

	if m2.filter.RawQuery != rawQuery {
		t.Errorf("RawQuery = %q, want %q", m2.filter.RawQuery, rawQuery)
	}
	if m2.filter.MinSeverity != 0 || m2.filter.LogName != "" {
		t.Errorf("structured fields not cleared: %+v", m2.filter)
	}
	if m2.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m2.mode)
	}
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected the submission to trigger a fetch")
	}
}

func TestApplyRawQuery_DefaultsSinceWhenZero(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.filter.Since = time.Time{} // simulate a zeroed Since

	updated, _ := m.applyRawQuery(`resource.type="k8s_container"`)
	if updated.filter.Since.IsZero() {
		t.Error("Since is still zero after applyRawQuery, want it resolved to a default lookback")
	}
}

func TestSaveLoadQueriesCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.filter.RawQuery = `resource.type="k8s_container"`

	updated, _ := m.Update(commandSubmittedMsg{cmd: parsedCommand{Kind: cmdSave, Arg: "k8s-test"}})
	m = updated.(appModel)
	if !strings.Contains(m.notice, "saved query k8s-test") {
		t.Errorf("notice = %q, want it to confirm the save", m.notice)
	}

	// Clear the active filter, then reload it by name.
	m.filter.RawQuery = ""
	updated, cmd := m.Update(commandSubmittedMsg{cmd: parsedCommand{Kind: cmdLoad, Arg: "k8s-test"}})
	m = updated.(appModel)
	if m.filter.RawQuery != `resource.type="k8s_container"` {
		t.Errorf("RawQuery after load = %q, want the saved query restored", m.filter.RawQuery)
	}
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected :load to trigger a fetch")
	}

	updated, _ = m.Update(commandSubmittedMsg{cmd: parsedCommand{Kind: cmdQueries}})
	m = updated.(appModel)
	if m.mode != modeQueries {
		t.Errorf("mode = %v, want modeQueries", m.mode)
	}
	if len(m.savedQueries) != 1 || m.savedQueries[0].Name != "k8s-test" {
		t.Errorf("savedQueries = %+v, want one entry named k8s-test", m.savedQueries)
	}
}

func TestLoadMissingQueryReportsNotice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New(&gcplogtest.Client{}, "test-project")

	updated, _ := m.Update(commandSubmittedMsg{cmd: parsedCommand{Kind: cmdLoad, Arg: "missing"}})
	m2 := updated.(appModel)
	if !strings.Contains(m2.notice, "no saved query named missing") {
		t.Errorf("notice = %q, want it to report the missing query", m2.notice)
	}
}

func TestQueriesSelectionAndRun(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.mode = modeQueries
	m.savedQueries = []config.SavedQuery{
		{Name: "a", Filter: "severity>=ERROR"},
		{Name: "b", Filter: "severity>=WARNING"},
	}

	updated, _ := m.Update(textKey("j"))
	m = updated.(appModel)
	if m.queriesSelected != 1 {
		t.Fatalf("queriesSelected = %d, want 1 after j", m.queriesSelected)
	}

	// Clamped at the last entry.
	updated, _ = m.Update(textKey("j"))
	m = updated.(appModel)
	if m.queriesSelected != 1 {
		t.Errorf("queriesSelected = %d, want clamped at 1", m.queriesSelected)
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(appModel)
	if m.filter.RawQuery != "severity>=WARNING" {
		t.Errorf("RawQuery = %q, want the selected (index 1) query", m.filter.RawQuery)
	}
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after running a query", m.mode)
	}
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected running a query to trigger a fetch")
	}
}

func TestPasteRoutesToQueryEditor(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.mode = modeQuery
	m.query.focus() // textarea.Update ignores all input, paste included, while unfocused

	pasted := `resource.type="k8s_container"`
	updated, _ := m.Update(tea.PasteMsg{Content: pasted})
	m2 := updated.(appModel)
	if got := m2.query.textarea.Value(); got != pasted {
		t.Errorf("query editor value = %q after paste, want %q", got, pasted)
	}
}

func TestRenderHeaderNeverWraps(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.width = 80
	m.filter.RawQuery = "resource.type=\"k8s_container\"\n" +
		"resource.labels.cluster_name=\"my-cluster\"\n" +
		"resource.labels.container_name=\"my-container\"\n" +
		"resource.labels.namespace_name=\"my-namespace\""

	// The header is deliberately headerLines (2) physical lines — the
	// project/status line, and a full-width line for the query — but a
	// raw query's own embedded newlines (or a multi-line notice) must
	// never grow it past exactly that, or the footer gets pushed off
	// the bottom of the screen (still there, just scrolled out of view).
	lines := strings.Split(m.renderHeader(), "\n")
	if len(lines) != headerLines {
		t.Fatalf("renderHeader() produced %d lines, want exactly %d: %q", len(lines), headerLines, lines)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("header line %d is %d cols wide, want <= %d: %q", i, w, m.width, line)
		}
	}
}

func TestRenderHeaderTopKeepsStatusVisibleWhenTight(t *testing.T) {
	// A long project name plus a status that together don't fit width —
	// this used to silently drop the whole right-hand status instead of
	// shrinking the project name.
	m := New(&gcplogtest.Client{}, "a-very-long-google-cloud-project-id-1234567890")
	m.width = 40
	m.list.entries = entries(5)

	top := m.renderHeaderTop()
	if w := lipgloss.Width(top); w > m.width {
		t.Errorf("header top line is %d cols wide, want <= %d: %q", w, m.width, top)
	}
	if !strings.Contains(top, "5 entries") {
		t.Errorf("header top line = %q, want the entry count still visible even when tight", top)
	}
}

// errFake is a sentinel error used where the test only cares that *an*
// error occurred, not its exact text.
var errFake = fakeErr("boom")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func TestFilterSubmittedTriggersHistogram(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80

	_, cmd := m.Update(filterSubmittedMsg{filter: gcplog.FilterState{MinSeverity: logging.Error}})
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected a filter submission to still trigger a list fetch")
	}
	if _, ok := findMsg[histogramLoadedMsg](cmd); !ok {
		t.Error("expected a filter submission to also trigger a histogram fetch")
	}
	if len(fake.HistogramCalls) != 1 {
		t.Errorf("HistogramCalls = %d, want 1", len(fake.HistogramCalls))
	}
}

func TestRefreshTriggersHistogram(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80

	_, cmd := m.Update(textKey("r"))
	if _, ok := findMsg[histogramLoadedMsg](cmd); !ok {
		t.Error("expected the refresh key to trigger a histogram fetch")
	}
}

func TestPaginationDoesNotTriggerHistogram(t *testing.T) {
	// Pagination doesn't move the filter's time window, so it must not
	// recompute the histogram — only triggerFetchAndHistogram call sites
	// should.
	fake := &gcplogtest.Client{Pages: []gcplog.Page{
		{Entries: []gcplog.Entry{{Timestamp: time.Now()}}, NextPageToken: "next"},
		{},
	}}
	m := New(fake, "test-project")
	m.width = 80
	loaded, _ := findMsg[entriesLoadedMsg](m.Init())
	updated, _ := m.Update(loaded)
	m = updated.(appModel)

	_, cmd := m.Update(textKey("n")) // NextPage
	if _, ok := findMsg[histogramLoadedMsg](cmd); ok {
		t.Error("pagination should not trigger a histogram fetch")
	}
	if len(fake.HistogramCalls) != 0 {
		t.Errorf("HistogramCalls = %d, want 0 after pagination", len(fake.HistogramCalls))
	}
}

func TestWindowSizeMsgTriggersHistogramFetchOnce(t *testing.T) {
	orig := histogramResizeDebounce
	histogramResizeDebounce = time.Millisecond
	defer func() { histogramResizeDebounce = orig }()

	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")

	// Both widths below gcplog.MaxHistogramBuckets, so bucket count == width
	// — a bucket-count-changing resize, not just a pixel-width one.
	m, cmd := settleHistogramResize(t, m, tea.WindowSizeMsg{Width: 40, Height: 24})
	loaded, ok := findMsg[histogramLoadedMsg](cmd)
	if !ok {
		t.Fatal("expected the first WindowSizeMsg to eventually trigger a histogram fetch")
	}
	updated, _ := m.Update(loaded)
	m = updated.(appModel)

	// A second WindowSizeMsg with the same width shouldn't refetch.
	m, cmd = settleHistogramResize(t, m, tea.WindowSizeMsg{Width: 40, Height: 30})
	if _, ok := findMsg[histogramLoadedMsg](cmd); ok {
		t.Error("a WindowSizeMsg with an unchanged width should not trigger another histogram fetch")
	}

	// A bucket-count-changing width should.
	_, cmd = settleHistogramResize(t, m, tea.WindowSizeMsg{Width: 55, Height: 30})
	if _, ok := findMsg[histogramLoadedMsg](cmd); !ok {
		t.Error("a WindowSizeMsg with a changed bucket count should trigger another histogram fetch")
	}
}

// TestWindowSizeMsgAboveBucketCapDoesNotRefetch covers the optimization
// histogramBucketCount exists for: on any terminal wider than
// gcplog.MaxHistogramBuckets columns, the bucket count is pinned at the
// cap, so resizing within that range (dragging a maximized window's edge a
// few columns, say) must not trigger a fresh histogram fetch at all.
func TestWindowSizeMsgAboveBucketCapDoesNotRefetch(t *testing.T) {
	orig := histogramResizeDebounce
	histogramResizeDebounce = time.Millisecond
	defer func() { histogramResizeDebounce = orig }()

	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")

	m, cmd := settleHistogramResize(t, m, tea.WindowSizeMsg{Width: 200, Height: 24})
	loaded, ok := findMsg[histogramLoadedMsg](cmd)
	if !ok {
		t.Fatal("expected the first WindowSizeMsg to eventually trigger a histogram fetch")
	}
	updated, _ := m.Update(loaded)
	m = updated.(appModel)

	_, cmd = settleHistogramResize(t, m, tea.WindowSizeMsg{Width: 250, Height: 24})
	if _, ok := findMsg[histogramLoadedMsg](cmd); ok {
		t.Error("a resize that doesn't change the (capped) bucket count should not trigger another histogram fetch")
	}
}

// settleHistogramResize applies a WindowSizeMsg and, if it scheduled a
// debounced histogram refetch (see histogramResizeSettledMsg), synchronously
// waits out the debounce (shrunk to ~0 by the caller — see
// histogramResizeDebounce) and applies the resulting message too,
// collapsing the two-step resize-then-fetch flow into one call.
func settleHistogramResize(t *testing.T, m appModel, resize tea.WindowSizeMsg) (appModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(resize)
	m = updated.(appModel)
	if cmd == nil {
		return m, nil
	}
	settled, ok := findMsg[histogramResizeSettledMsg](cmd)
	if !ok {
		return m, cmd
	}
	updated, fetchCmd := m.Update(settled)
	return updated.(appModel), fetchCmd
}

func TestHistogramRangeSelectedAppliesRangeAndStopsTail(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeTail
	m.tailCancel = func() {}

	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	updated, cmd := m.Update(histogramRangeSelectedMsg{since: since, until: until})
	m2 := updated.(appModel)

	if m2.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after a histogram range selection", m2.mode)
	}
	if !m2.filter.Since.Equal(since) || !m2.filter.Until.Equal(until) {
		t.Errorf("filter = %+v, want Since=%v Until=%v", m2.filter, since, until)
	}
	if m2.tailCancel != nil {
		t.Error("expected tailing to be stopped by a histogram range selection")
	}
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected a histogram range selection to trigger a list fetch")
	}
}

func TestShowsHistogramHiddenBelowMinWidth(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.width = minHistogramWidth - 1
	if m.showsHistogram() {
		t.Error("showsHistogram() = true below minHistogramWidth, want false")
	}
	m.width = minHistogramWidth
	if !m.showsHistogram() {
		t.Error("showsHistogram() = false at minHistogramWidth, want true")
	}
}

func TestShowsHistogramHiddenInDetailMode(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.width = 80
	m.mode = modeDetail
	if m.showsHistogram() {
		t.Error("showsHistogram() = true in modeDetail, want false")
	}
}

func TestFacetsKeyOpensPanelAndFetchesFullRange(t *testing.T) {
	fake := &gcplogtest.Client{
		FacetsResult: gcplog.FacetsResult{Facets: gcplog.Facets{
			Severity: []gcplog.SeverityFacetCount{{Severity: logging.Warning, Count: 3}},
		}},
	}
	m := New(fake, "test-project")
	m.width = 80

	updated, cmd := m.Update(textKey("f"))
	m = updated.(appModel)
	if m.mode != modeFacetFocus {
		t.Errorf("mode = %v, want modeFacetFocus", m.mode)
	}
	if !m.facets.open {
		t.Error("expected facets.open = true")
	}

	// FacetsCalls is only recorded once the Cmd returned by the f press
	// actually runs (Cmds are lazy) — findMsg below is what runs it.
	loaded, ok := findMsg[facetsLoadedMsg](cmd)
	if !ok {
		t.Fatal("expected pressing f to trigger a Client.Facets fetch")
	}
	if len(fake.FacetsCalls) != 1 {
		t.Fatalf("FacetsCalls = %d, want 1", len(fake.FacetsCalls))
	}
	updated, _ = m.Update(loaded)
	m = updated.(appModel)
	if len(m.facets.facets.Severity) != 1 {
		t.Errorf("facets.facets.Severity = %+v, want 1 entry", m.facets.facets.Severity)
	}
}

func TestFacetsStaleGenerationIgnored(t *testing.T) {
	m := New(&gcplogtest.Client{}, "test-project")
	m.width = 80
	m.facetGen = 5

	updated, _ := m.Update(facetsLoadedMsg{gen: 3, result: gcplog.FacetsResult{Facets: sampleFacets()}})
	m2 := updated.(appModel)
	if len(m2.facets.rows) != 0 {
		t.Errorf("expected a stale-gen facetsLoadedMsg to be ignored, got rows=%+v", m2.facets.rows)
	}
}

func TestFacetsKeyOutsideBrowseOrTailIsNoop(t *testing.T) {
	fake := &gcplogtest.Client{}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeDetail

	updated, _ := m.Update(textKey("f"))
	m2 := updated.(appModel)
	if m2.facets.open || m2.mode != modeDetail {
		t.Errorf("expected f to be a no-op outside browse/tail, got open=%v mode=%v", m2.facets.open, m2.mode)
	}
	if len(fake.FacetsCalls) != 0 {
		t.Errorf("FacetsCalls = %d, want 0", len(fake.FacetsCalls))
	}
}

func TestFacetsSecondPressWhileFocusedCloses(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80

	updated, _ := m.Update(textKey("f"))
	m = updated.(appModel)
	if !m.facets.open {
		t.Fatal("expected facets to be open after the first f press")
	}

	updated, _ = m.Update(textKey("f"))
	m = updated.(appModel)
	if m.facets.open {
		t.Error("expected a second f press while focused to close the panel")
	}
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after closing", m.mode)
	}
}

func TestFacetsEscUnfocusesWithoutClosing(t *testing.T) {
	fake := &gcplogtest.Client{}
	m := New(fake, "test-project")
	m.width = 80

	updated, _ := m.Update(textKey("f"))
	m = updated.(appModel)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(appModel)
	if !m.facets.open {
		t.Error("esc should not close the panel, only unfocus it")
	}
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after esc", m.mode)
	}

	// Pressing f again should just refocus, not refetch.
	fake.FacetsCalls = nil
	updated, _ = m.Update(textKey("f"))
	m = updated.(appModel)
	if m.mode != modeFacetFocus {
		t.Errorf("mode = %v, want modeFacetFocus after refocusing", m.mode)
	}
	if len(fake.FacetsCalls) != 0 {
		t.Errorf("refocusing an already-open panel should not refetch, got %d calls", len(fake.FacetsCalls))
	}
}

func TestApplyFacetFilterSeverity(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeFacetFocus
	m.facets.returnMode = modeBrowse
	m.filter.MinSeverity = logging.Error

	updated, cmd := m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldSeverity, severity: logging.Warning, count: 3}})
	m = updated.(appModel)

	if m.filter.ExactSeverity != logging.Warning {
		t.Errorf("ExactSeverity = %v, want Warning", m.filter.ExactSeverity)
	}
	if m.filter.MinSeverity != 0 {
		t.Errorf("MinSeverity = %v, want cleared", m.filter.MinSeverity)
	}
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m.mode)
	}
	if _, ok := findMsg[entriesLoadedMsg](cmd); !ok {
		t.Error("expected applying a facet filter to trigger a list fetch")
	}
}

func TestApplyFacetFilterLabelStacksAcrossKeys(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeFacetFocus
	m.facets.returnMode = modeBrowse

	updated, _ := m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldLabel, labelKey: "env", value: "prod"}})
	m = updated.(appModel)
	m.mode = modeFacetFocus // applying returns to modeBrowse; re-focus for the second click

	updated, _ = m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldLabel, labelKey: "zone", value: "us-east1"}})
	m = updated.(appModel)

	want := map[string]string{"env": "prod", "zone": "us-east1"}
	if !reflect.DeepEqual(m.filter.Labels, want) {
		t.Errorf("Labels = %+v, want %+v (should stack, not replace)", m.filter.Labels, want)
	}
}

func TestApplyFacetFilterResourceAndLogName(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeFacetFocus
	m.facets.returnMode = modeBrowse

	updated, _ := m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldResource, value: "k8s_container"}})
	m = updated.(appModel)
	if m.filter.ResourceType != "k8s_container" {
		t.Errorf("ResourceType = %q, want k8s_container", m.filter.ResourceType)
	}

	m.mode = modeFacetFocus
	updated, _ = m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldLogName, value: "syslog"}})
	m = updated.(appModel)
	if m.filter.LogName != "syslog" {
		t.Errorf("LogName = %q, want syslog", m.filter.LogName)
	}
}

func TestApplyFacetFilterFromTailRestartsTail(t *testing.T) {
	fake := &gcplogtest.Client{}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeFacetFocus
	m.facets.returnMode = modeTail
	m.facets.open = true

	updated, cmd := m.Update(facetValueSelectedMsg{row: facetRow{field: facetFieldResource, value: "gce_instance"}})
	m = updated.(appModel)

	if m.mode != modeTail {
		t.Errorf("mode = %v, want modeTail", m.mode)
	}
	if m.filter.ResourceType != "gce_instance" {
		t.Errorf("ResourceType = %q, want gce_instance", m.filter.ResourceType)
	}
	started, ok := findMsg[tailStartedMsg](cmd)
	if !ok {
		t.Fatal("expected applying a facet filter from tail to (re)start the tail stream")
	}
	updated, _ = m.Update(started)
	m = updated.(appModel)
	if m.tailCancel == nil {
		t.Error("expected the tail stream to have been started")
	}
}

// TestTailFacetsAggregateSynchronouslyWithoutFetch covers the tail-mode
// facet path: counts come straight from the already-accumulated tail
// buffer, with no Client.Facets call and no async fetch.
func TestTailFacetsAggregateSynchronouslyWithoutFetch(t *testing.T) {
	fake := &gcplogtest.Client{}
	m := New(fake, "test-project")
	m.width = 80
	m.mode = modeTail
	m.tail.entries = []gcplog.Entry{
		{Severity: logging.Warning, LogName: "syslog"},
		{Severity: logging.Warning, LogName: "syslog"},
	}

	updated, cmd := m.Update(textKey("f"))
	m = updated.(appModel)

	if !m.facets.open || m.mode != modeFacetFocus {
		t.Fatalf("expected the panel to open and focus, got open=%v mode=%v", m.facets.open, m.mode)
	}
	if len(fake.FacetsCalls) != 0 {
		t.Errorf("tail-mode facets should not call Client.Facets, got %d calls", len(fake.FacetsCalls))
	}
	if _, ok := findMsg[facetsLoadedMsg](cmd); ok {
		t.Error("tail-mode facets should resolve synchronously, not via a Cmd")
	}
	if len(m.facets.facets.Severity) != 1 || m.facets.facets.Severity[0].Count != 2 {
		t.Errorf("facets.facets.Severity = %+v, want one Warning entry with count 2", m.facets.facets.Severity)
	}
}

// TestFilterBarPreservesFacetDerivedFieldsAcrossSubmission is the
// regression test for filterBarModel.build()'s fix: submitting the filter
// bar must not silently wipe fields it has no control over (ResourceType,
// Labels, ExactSeverity) that a prior facet click set.
func TestFilterBarPreservesFacetDerivedFieldsAcrossSubmission(t *testing.T) {
	fake := &gcplogtest.Client{Pages: []gcplog.Page{{}}}
	m := New(fake, "test-project")
	m.width = 80
	m.filter.ResourceType = "k8s_container"
	m.filter.Labels = map[string]string{"env": "prod"}
	m.filter.ExactSeverity = logging.Warning
	m.filter.Until = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	// Open the bar (seeds it from m.filter) and submit unchanged.
	updated, _ := m.Update(textKey("/"))
	m = updated.(appModel)
	updated, cmd := m.Update(textKey("enter"))
	m = updated.(appModel)
	submitted, ok := findMsg[filterSubmittedMsg](cmd)
	if !ok {
		t.Fatal("expected enter in the filter bar to submit")
	}
	updated, _ = m.Update(submitted)
	m = updated.(appModel)

	if m.filter.ResourceType != "k8s_container" {
		t.Errorf("ResourceType = %q, want preserved k8s_container", m.filter.ResourceType)
	}
	if !reflect.DeepEqual(m.filter.Labels, map[string]string{"env": "prod"}) {
		t.Errorf("Labels = %+v, want preserved", m.filter.Labels)
	}
	if !m.filter.Until.Equal(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("Until = %v, want preserved", m.filter.Until)
	}
	// Submitting the bar still means "structured filtering now": a
	// MinSeverity threshold submit clears any stale exact-match.
	if m.filter.ExactSeverity != 0 {
		t.Errorf("ExactSeverity = %v, want cleared by a bar submission", m.filter.ExactSeverity)
	}
}
