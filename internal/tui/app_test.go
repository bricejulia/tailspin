package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

	if header := m.renderHeader(); strings.Contains(header, "\n") {
		t.Errorf("renderHeader() contains a newline, want exactly one physical line: %q", header)
	}
}

// errFake is a sentinel error used where the test only cares that *an*
// error occurred, not its exact text.
var errFake = fakeErr("boom")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
