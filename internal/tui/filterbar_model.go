package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"cloud.google.com/go/logging"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// severityOptions are the thresholds cyclable in the filter bar's severity
// field, from "any" up to "critical and above".
var severityOptions = []struct {
	label string
	value logging.Severity
}{
	{"any", logging.Default},
	{"debug", logging.Debug},
	{"info", logging.Info},
	{"warning", logging.Warning},
	{"error", logging.Error},
	{"critical", logging.Critical},
}

// sinceOptions are the time-range presets cyclable in the filter bar's
// "since" field, capped at 1d — a wider range multiplies the read
// requests a histogram/facet fetch over it costs (see histogramTimeout),
// so there's no "all time" option here.
var sinceOptions = []struct {
	label string
	value time.Duration
}{
	{"1m", time.Minute},
	{"5m", 5 * time.Minute},
	{"10m", 10 * time.Minute},
	{"15m", 15 * time.Minute},
	{"30m", 30 * time.Minute},
	{"45m", 45 * time.Minute},
	{"1h", time.Hour},
	{"3h", 3 * time.Hour},
	{"6h", 6 * time.Hour},
	{"12h", 12 * time.Hour},
	{"1d", 24 * time.Hour},
}

// filterField identifies which control in the filter bar has focus.
type filterField int

const (
	fieldSeverity filterField = iota
	fieldLogName
	fieldFreeText
	fieldSince
	fieldCount // sentinel: number of fields, for wraparound
)

// filterBarModel is the "/" filter bar: severity and time-range are small
// cyclable enums (left/right), log name and free text are plain text
// inputs. Tab/shift+tab moves focus between the four fields.
type filterBarModel struct {
	focus filterField

	// base is the FilterState this bar was last seeded from (see seed).
	// build() starts from it and overrides only the fields the bar
	// actually has controls for (MinSeverity, LogName, FreeText, Since),
	// so fields it doesn't — ResourceType, Labels, ExactSeverity, Until,
	// all only ever set by the facet panel — survive a bar submission
	// unchanged instead of being silently wiped by a bar the user never
	// touched them through.
	base gcplog.FilterState

	severityIdx int
	sinceIdx    int
	logName     textinput.Model
	freeText    textinput.Model
}

func newFilterBarModel() filterBarModel {
	logName := textinput.New()
	logName.Prompt = "log:"
	logName.Placeholder = "syslog"
	logName.SetWidth(20)

	freeText := textinput.New()
	freeText.Prompt = "text:"
	freeText.Placeholder = "search…"
	freeText.SetWidth(24)

	return filterBarModel{
		logName:  logName,
		freeText: freeText,
		sinceIdx: 1, // 5m, matching browse mode's default lookback
	}
}

// seed primes the filter bar's fields from an existing FilterState (e.g.
// when reopening the bar to tweak the currently-active filter), and stashes
// the whole thing as base so build() can preserve whatever fields the bar
// itself has no control over.
func (m *filterBarModel) seed(f gcplog.FilterState) {
	m.base = f
	m.logName.SetValue(f.LogName)
	m.freeText.SetValue(f.FreeText)
	for i, opt := range severityOptions {
		if opt.value == f.MinSeverity {
			m.severityIdx = i
		}
	}
}

// focusField returns a Cmd that focuses the currently-selected field (for
// the text fields; the enum fields have no text cursor to focus).
func (m *filterBarModel) focusField() tea.Cmd {
	m.logName.Blur()
	m.freeText.Blur()
	switch m.focus {
	case fieldLogName:
		return m.logName.Focus()
	case fieldFreeText:
		return m.freeText.Focus()
	default:
		// fieldSeverity, fieldSince: enum fields, nothing to focus.
	}
	return nil
}

// filterSubmittedMsg is emitted when the user presses enter in the filter
// bar with a built FilterState ready to query.
type filterSubmittedMsg struct {
	filter gcplog.FilterState
}

func (m filterBarModel) Update(msg tea.Msg) (filterBarModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, tabKey):
			m.focus = (m.focus + 1) % fieldCount
			return m, m.focusField()
		case key.Matches(keyMsg, shiftTabKey):
			m.focus = (m.focus - 1 + fieldCount) % fieldCount
			return m, m.focusField()
		case keyMsg.String() == "enter":
			return m, func() tea.Msg { return filterSubmittedMsg{filter: m.build()} }
		}

		switch m.focus {
		case fieldSeverity:
			switch keyMsg.String() {
			case "left", "h":
				m.severityIdx = (m.severityIdx - 1 + len(severityOptions)) % len(severityOptions)
				return m, nil
			case "right", "l":
				m.severityIdx = (m.severityIdx + 1) % len(severityOptions)
				return m, nil
			}
		case fieldSince:
			switch keyMsg.String() {
			case "left", "h":
				m.sinceIdx = (m.sinceIdx - 1 + len(sinceOptions)) % len(sinceOptions)
				return m, nil
			case "right", "l":
				m.sinceIdx = (m.sinceIdx + 1) % len(sinceOptions)
				return m, nil
			}
		default:
			// fieldLogName, fieldFreeText: text input, handled below.
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case fieldLogName:
		m.logName, cmd = m.logName.Update(msg)
	case fieldFreeText:
		m.freeText, cmd = m.freeText.Update(msg)
	default:
		// fieldSeverity, fieldSince: enum fields, no text input to update.
	}
	return m, cmd
}

// build renders the filter bar's current field values as a FilterState,
// starting from base (see its doc comment) so fields the bar has no
// control for survive a submit unchanged. RawQuery and Since are always
// explicitly reset here rather than left as whatever base carried: a bar
// submission means "I'm using structured filtering now" (RawQuery), and
// Since is fully re-derived from the bar's own since-preset (no partial
// carry-over of a prior absolute value).
func (m filterBarModel) build() gcplog.FilterState {
	f := m.base
	f.MinSeverity = severityOptions[m.severityIdx].value
	f.ExactSeverity = 0 // a submitted threshold shouldn't be shadowed by a stale exact match from an earlier facet click
	f.LogName = m.logName.Value()
	f.FreeText = m.freeText.Value()
	f.RawQuery = ""
	f.Since = time.Time{}
	if d := sinceOptions[m.sinceIdx].value; d > 0 {
		f.Since = time.Now().Add(-d)
	}
	return f
}

func (m filterBarModel) View() string {
	sev := "sev:" + severityOptions[m.severityIdx].label
	since := "since:" + sinceOptions[m.sinceIdx].label
	fields := []struct {
		field filterField
		view  string
	}{
		{fieldSeverity, sev},
		{fieldLogName, m.logName.View()},
		{fieldFreeText, m.freeText.View()},
		{fieldSince, since},
	}

	var out strings.Builder
	for i, f := range fields {
		if i > 0 {
			out.WriteString("  ")
		}
		if f.field == m.focus {
			out.WriteString(selectedRowStyle.Render(f.view))
		} else {
			out.WriteString(f.view)
		}
	}
	return out.String()
}
