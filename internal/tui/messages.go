package tui

import (
	"time"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

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

// histogramLoadedMsg reports the result of a Histogram fetch triggered by
// fetchHistogramCmd.
type histogramLoadedMsg struct {
	result     gcplog.HistogramResult
	forBuckets int // the bucket count the fetch was requested with
	err        error
}

// histogramRangeSelectedMsg is emitted by histogramModel when a click or
// completed drag on the chart selects a time range — the histogram's
// equivalent of filterSubmittedMsg.
type histogramRangeSelectedMsg struct {
	since, until time.Time
}

// histogramResizeSettledMsg fires histogramResizeDebounce after a
// WindowSizeMsg that changed the histogram's bucket count — see there. gen
// is compared against appModel.histogramResizeGen so only the most recent
// resize in a burst actually triggers a fetch.
type histogramResizeSettledMsg struct {
	gen int
}
