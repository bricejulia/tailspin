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
// "since" field. A zero duration means "all time" (no lower bound).
var sinceOptions = []struct {
	label string
	value time.Duration
}{
	{"1h", time.Hour},
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
	{"all", 0},
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
		sinceIdx: 1, // 24h, matching browse mode's default lookback
	}
}

// seed primes the filter bar's fields from an existing FilterState (e.g.
// when reopening the bar to tweak the currently-active filter).
func (m *filterBarModel) seed(f gcplog.FilterState) {
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

// build renders the filter bar's current field values as a FilterState.
func (m filterBarModel) build() gcplog.FilterState {
	f := gcplog.FilterState{
		MinSeverity: severityOptions[m.severityIdx].value,
		LogName:     m.logName.Value(),
		FreeText:    m.freeText.Value(),
	}
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
