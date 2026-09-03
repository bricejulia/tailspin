package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"cloud.google.com/go/logging"

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

// errFake is a sentinel error used where the test only cares that *an*
// error occurred, not its exact text.
var errFake = fakeErr("boom")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
