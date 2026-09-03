package tui

import "github.com/bricejulia/tailspin/internal/gcplog"

// entriesLoadedMsg reports the result of an (initial or paginated) list
// fetch triggered by fetchPage.
type entriesLoadedMsg struct {
	page      gcplog.Page
	appending bool // true when this page should extend the list rather than replace it
	err       error
}
