package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// TestTailModelEmptyViewFillsHeight guards against the "waiting for log
// entries…" message collapsing to a single line: whatever renders after it
// (the footer) should stay pinned to the bottom of the terminal, not float
// up right underneath the header.
func TestTailModelEmptyViewFillsHeight(t *testing.T) {
	m := newTailModel()
	m.SetSize(40, 12)

	if got := lipgloss.Height(m.View()); got != 12 {
		t.Errorf("Height(View()) = %d, want 12 (the allocated box)", got)
	}
}
