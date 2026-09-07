package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"cloud.google.com/go/logging"

	"github.com/bricejulia/tailspin/internal/gcplog"
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

	// selectedRowStyle is applied once, over the whole selected row's
	// plain (unstyled) text — see entryPrefixPlain's doc comment for why
	// it can't be layered on top of already-Render()'d, self-resetting
	// column spans instead.
	selectedRowStyle = lipgloss.NewStyle().
				Background(colorSelectBg).
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

	// plainStyle applies no color at all — the terminal's own default
	// foreground, which reads with more contrast than headerMetaStyle's
	// muted gray. Used for the header's query line: now that the query
	// has a full-width line of its own rather than being squeezed into
	// the middle of a busier line, it's the main thing being read there.
	plainStyle = lipgloss.NewStyle()

	// facetPanelStyle draws the facet side panel's left border — the one
	// deliberate exception to "no boxes or borders" above, needed to
	// visually separate the panel from the log list it sits beside now
	// that the layout has its first horizontal split.
	facetPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorSubtle).
			PaddingLeft(1)
)

// tierColor maps gcplog.ClassifySeverity's bands to the palette above — the
// one place list rows, the detail view, and the histogram's stacked bars all
// get their severity color from, so the three can never drift apart.
var tierColor = [gcplog.NumSeverityTiers]color.Color{
	gcplog.SeverityTierDefault: colorMuted,
	gcplog.SeverityTierInfo:    colorInfo,
	gcplog.SeverityTierWarning: colorWarning,
	gcplog.SeverityTierError:   colorError,
	gcplog.SeverityTierAlert:   colorAlert,
}

// severityStyle returns the k9s-style status color for a log severity.
func severityStyle(s logging.Severity) lipgloss.Style {
	tier := gcplog.ClassifySeverity(s)
	return lipgloss.NewStyle().Bold(tier >= gcplog.SeverityTierError).Foreground(tierColor[tier])
}
