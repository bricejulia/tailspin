package tui

import "github.com/bricejulia/tailspin/internal/gcplog"

// entriesLoadedMsg reports the result of an (initial or paginated) list
// fetch triggered by fetchPage.
type entriesLoadedMsg struct {
	page      gcplog.Page
	appending bool // true when this page should extend the list rather than replace it
	err       error
}

// projectSwitchedMsg reports the result of opening a new gcplog.Client for
// a project, triggered by the ":project <id>" command.
type projectSwitchedMsg struct {
	client  gcplog.Client
	project string
	err     error
}

// tailStartedMsg reports the result of opening a live tail stream. gen
// identifies which tail session this belongs to (see appModel.tailGen) so
// a stale start/event from a session the user has already left doesn't get
// applied to whatever's current.
type tailStartedMsg struct {
	gen    int
	events <-chan gcplog.TailEvent
	cancel func()
	err    error
}

// tailEventMsg delivers one event from an active tail stream's channel,
// via waitForTailEvent.
type tailEventMsg struct {
	gen   int
	event gcplog.TailEvent
}
