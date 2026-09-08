package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/bricejulia/tailspin/internal/gcplog/gcplogtest"
)

func TestJoinFooterPiecesFitsUnclipped(t *testing.T) {
	pieces := []string{"<a> aaa", "<b> bbb", "<c> ccc"}
	joined := strings.Join(pieces, "  ")

	got := joinFooterPieces(pieces, lipgloss.Width(joined))
	if got != joined {
		t.Errorf("joinFooterPieces() = %q, want %q (fits exactly)", got, joined)
	}
}

func TestJoinFooterPiecesUnknownWidthRendersEverything(t *testing.T) {
	pieces := []string{"<a> aaa", "<b> bbb", "<c> ccc"}
	joined := strings.Join(pieces, "  ")

	if got := joinFooterPieces(pieces, 0); got != joined {
		t.Errorf("joinFooterPieces(pieces, 0) = %q, want %q (width unknown, no clipping)", got, joined)
	}
	if got := joinFooterPieces(pieces, -1); got != joined {
		t.Errorf("joinFooterPieces(pieces, -1) = %q, want %q (width unknown, no clipping)", got, joined)
	}
}

// TestJoinFooterPiecesClipsRatherThanOverflow is this whole helper's reason
// to exist: given a width too narrow for every piece, it must never return
// something wider than that budget — a footer line wider than the
// terminal risks the terminal auto-wrapping it, pushing the footer (the
// last line rendered, with nothing below it) off the bottom of the screen
// entirely instead of just looking cluttered.
func TestJoinFooterPiecesClipsRatherThanOverflow(t *testing.T) {
	pieces := []string{"<a> aaa", "<b> bbb", "<c> ccc", "<d> ddd"}
	joined := strings.Join(pieces, "  ")
	full := lipgloss.Width(joined)

	for width := 1; width < full; width++ {
		got := joinFooterPieces(pieces, width)
		if w := lipgloss.Width(got); w > width {
			t.Fatalf("joinFooterPieces(pieces, %d) = %q (width %d), want width <= %d", width, got, w, width)
		}
	}
}

func TestJoinFooterPiecesMarksTruncationWithEllipsis(t *testing.T) {
	pieces := []string{"<a> aaa", "<b> bbb", "<c> ccc", "<d> ddd"}
	joined := strings.Join(pieces, "  ")

	got := joinFooterPieces(pieces, lipgloss.Width(joined)-1)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("joinFooterPieces() = %q, want it to end with an ellipsis once a piece had to be dropped", got)
	}
	if got == joined {
		t.Error("expected at least one piece to be dropped for a width one short of the full line")
	}
}

// TestRenderFooterNeverExceedsWidth exercises the real key bindings for
// every mode (the default/browse case is the longest — see renderFooter)
// at a handful of realistic terminal widths, guarding against exactly the
// regression that motivated joinFooterPieces: adding one more keybinding
// to a mode's hint list (as keys.Histogram's toggle did) silently pushing
// the rendered line past the terminal width and off-screen.
func TestRenderFooterNeverExceedsWidth(t *testing.T) {
	modes := []mode{modeBrowse, modeDetail, modeTail, modeQuery, modeQueries, modeFacetFocus, modeError}
	for _, width := range []int{40, 60, 80, 100, 120, 200} {
		for _, md := range modes {
			m := New(&gcplogtest.Client{}, "test-project")
			m.width = width
			m.mode = md
			if got := lipgloss.Width(m.renderFooter()); got > width {
				t.Errorf("mode %v at width %d: renderFooter() width = %d, want <= %d", md, width, got, width)
			}
		}
	}
}
