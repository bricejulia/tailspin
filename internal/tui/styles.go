package tui

import (
	"charm.land/lipgloss/v2"
	"cloud.google.com/go/logging"
)

// Palette: k9s-inspired — color is the primary status signal, chrome stays
// minimal (a header bar and a footer bar, no boxes or borders around the
// list itself).
var (
	colorAccent   = lipgloss.Color("39")  // cyan — headers, focus, the ":" prompt
	colorMuted    = lipgloss.Color("244") // dim gray — secondary text, DEBUG/DEFAULT
	colorSubtle   = lipgloss.Color("240") // dimmer gray — footer key descriptions
	colorInfo     = lipgloss.Color("111") // soft blue — INFO/NOTICE
	colorWarning  = lipgloss.Color("221") // yellow — WARNING
	colorError    = lipgloss.Color("203") // red — ERROR/CRITICAL
	colorAlert    = lipgloss.Color("213") // magenta — ALERT/EMERGENCY
	colorLive     = lipgloss.Color("46")  // green — the live-tail indicator
	colorSelectBg = lipgloss.Color("24")  // selected row background — a real, unmistakable highlight
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	headerMetaStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	liveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLive)

	footerKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	footerDescStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorSelectBg).
				Bold(true)

	// selectedMarkerStyle is the left-gutter ">" glyph marking the
	// selected row — a second, color-independent cue on top of the
	// background so the selection reads clearly even on themes/profiles
	// where the background tint alone is subtle. Plain ASCII: an earlier
	// attempt with "▎" (U+258E) rendered as the literal text "\u{258e}"
	// under some terminfo/width-table combination instead of the glyph.
	selectedMarkerStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorError)

	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWarning)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	detailLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)
)

// severityStyle returns the k9s-style status color for a log severity.
func severityStyle(s logging.Severity) lipgloss.Style {
	switch {
	case s >= logging.Alert:
		return lipgloss.NewStyle().Bold(true).Foreground(colorAlert)
	case s >= logging.Error:
		return lipgloss.NewStyle().Bold(true).Foreground(colorError)
	case s >= logging.Warning:
		return lipgloss.NewStyle().Foreground(colorWarning)
	case s >= logging.Info:
		return lipgloss.NewStyle().Foreground(colorInfo)
	default:
		return lipgloss.NewStyle().Foreground(colorMuted)
	}
}
