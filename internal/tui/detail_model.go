package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// detailModel is the full-entry view opened with enter: metadata (severity,
// log name, resource, labels) followed by the pretty-printed payload. No
// second API call — logadmin.Entries already returns the full entry, so
// this just re-renders what's already in memory.
type detailModel struct {
	viewport viewport.Model
	entry    gcplog.Entry
}

func newDetailModel() detailModel {
	return detailModel{viewport: viewport.New()}
}

func (m *detailModel) SetSize(width, height int) {
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
}

// show loads entry into the detail view and scrolls to the top.
func (m *detailModel) show(entry gcplog.Entry) {
	m.entry = entry
	m.viewport.SetXOffset(0)
	m.viewport.SetContent(renderEntryDetail(entry))
	m.viewport.SetYOffset(0)
}

func (m detailModel) Update(msg tea.Msg) (detailModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, keys.Down):
			m.viewport.ScrollDown(1)
			return m, nil
		case key.Matches(keyMsg, keys.Up):
			m.viewport.ScrollUp(1)
			return m, nil
		case key.Matches(keyMsg, keys.PageDown):
			m.viewport.PageDown()
			return m, nil
		case key.Matches(keyMsg, keys.PageUp):
			m.viewport.PageUp()
			return m, nil
		}
	}
	return m, nil
}

func (m detailModel) View() string {
	return m.viewport.View()
}

// renderEntryDetail renders entry as a k9s-describe-style block: labeled
// metadata fields, a labels table, then the pretty-printed payload.
func renderEntryDetail(e gcplog.Entry) string {
	var b strings.Builder

	field := func(name, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s %s\n", detailLabelStyle.Render(padToWidth(name+":", 12)), value)
	}

	field("Time", e.Timestamp.Local().Format("2006-01-02 15:04:05 MST"))
	field("Severity", severityStyle(e.Severity).Render(strings.ToUpper(e.Severity.String())))
	field("Log", e.LogName)
	field("Resource", e.Resource)

	if len(e.Labels) > 0 {
		b.WriteString("\n")
		b.WriteString(detailLabelStyle.Render("Labels:"))
		b.WriteString("\n")
		labelKeys := make([]string, 0, len(e.Labels))
		for k := range e.Labels {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)
		for _, k := range labelKeys {
			fmt.Fprintf(&b, "  %s: %s\n", k, e.Labels[k])
		}
	}

	b.WriteString("\n")
	b.WriteString(detailLabelStyle.Render("Payload:"))
	b.WriteString("\n")
	b.WriteString(renderPayload(e.Payload))

	return b.String()
}

// renderPayload pretty-prints a log entry's payload: plain text as-is
// (textPayload), anything else as indented JSON (jsonPayload/protoPayload).
func renderPayload(payload any) string {
	if payload == nil {
		return statusStyle.Render("(empty)")
	}
	if s, ok := payload.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return statusStyle.Render(fmt.Sprintf("(unable to render payload: %s)", err))
	}
	return string(b)
}
