package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// renderFooter draws tailspin's one-line k9s-style hotkey menu for the
// current mode. The full hint list has grown past what fits on one line at
// ordinary terminal widths (see joinFooterPieces) — cramming another
// binding in here without checking that isn't free, it risks the terminal
// auto-wrapping this line and pushing itself (there's nothing below the
// footer) off the bottom of the screen, "still there and responding to
// input, just scrolled out of view" the same way header.go once described
// for its own overflow case.
func (m appModel) renderFooter() string {
	var bindings []key.Binding
	switch m.mode {
	case modeError:
		bindings = []key.Binding{keys.Refresh, keys.Command, keys.Back, keys.Quit}
	case modeDetail:
		bindings = []key.Binding{keys.Up, keys.Down, keys.PageUp, keys.PageDown, keys.Back, keys.Quit}
	case modeTail:
		bindings = []key.Binding{pauseKey, keys.Up, keys.Down, keys.Facets, keys.Histogram, keys.Back, keys.Quit}
	case modeQuery:
		bindings = []key.Binding{querySubmitKey, keys.Back}
	case modeQueries:
		bindings = []key.Binding{keys.Up, keys.Down, runQueryKey, keys.Back, keys.Quit}
	case modeFacetFocus:
		bindings = []key.Binding{keys.Up, keys.Down, facetApplyKey, keys.Refresh, keys.Facets, keys.Back}
	default:
		bindings = []key.Binding{keys.Up, keys.Down, pageNavKey, keys.Enter, keys.Filter, keys.Facets, keys.Histogram, keys.Command, keys.Tail, keys.Wrap, keys.Refresh, keys.Help, keys.Quit}
	}

	pieces := make([]string, len(bindings))
	for i, b := range bindings {
		h := b.Help()
		pieces[i] = footerKeyStyle.Render("<"+h.Key+">") + " " + footerDescStyle.Render(h.Desc)
	}
	return joinFooterPieces(pieces, m.width)
}

// joinFooterPieces joins pre-rendered "<key> desc" pieces with a two-space
// separator, the same as a plain strings.Join(pieces, "  ") — except it
// stops short and marks the cut with an ellipsis rather than let the
// joined line exceed width. width <= 0 means "unknown" (no WindowSizeMsg
// has landed yet, e.g. in a test that never sets it) — that always
// renders everything unclipped rather than guess.
//
// Measured with lipgloss.Width, not len/byte-slicing: pieces are already
// ANSI-styled and may contain multi-byte runes (the arrow glyphs in
// keys.Up/Down's help text), and byte-slicing a styled string risks
// cutting an escape sequence in half and corrupting everything rendered
// after it — the same hazard renderHeaderTop's own overflow fallback
// avoids. Hints are independent of each other, though, so this only ever
// needs to drop whole trailing pieces, never truncate text mid-hint the
// way the header's single line sometimes must.
func joinFooterPieces(pieces []string, width int) string {
	joined := strings.Join(pieces, "  ")
	if width <= 0 || lipgloss.Width(joined) <= width {
		return joined
	}

	const ellipsis = "…"
	budget := width - lipgloss.Width(ellipsis) - 1 // -1 reserves the space in front of it
	var out strings.Builder
	used := 0
	for i, p := range pieces {
		add := lipgloss.Width(p)
		sep := 0
		if i > 0 {
			sep = 2
		}
		if used+sep+add > budget {
			break
		}
		if i > 0 {
			out.WriteString("  ")
		}
		out.WriteString(p)
		used += sep + add
	}
	if out.Len() > 0 {
		out.WriteString(" ")
	}
	out.WriteString(ellipsis)
	return out.String()
}
