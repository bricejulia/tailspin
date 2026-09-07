package tui

import (
	"errors"
	"testing"

	"cloud.google.com/go/logging"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

func sampleFacets() gcplog.Facets {
	return gcplog.Facets{
		Severity: []gcplog.SeverityFacetCount{
			{Severity: logging.Warning, Count: 5},
			{Severity: logging.Error, Count: 2},
		},
		LogName: []gcplog.FacetCount{{Value: "syslog", Count: 4}},
		Labels: []gcplog.LabelFacet{
			{Key: "env", Values: []gcplog.FacetCount{{Value: "prod", Count: 3}, {Value: "staging", Count: 1}}},
		},
	}
}

func TestFacetModelBuildRowsHeadersThenValues(t *testing.T) {
	var m facetModel
	m.setResult(sampleFacets(), false)

	// Severity header, 2 severities, Log Name header, 1 log name, Labels:
	// env header, 2 values = 8 lines.
	if got, want := len(m.rows), 8; got != want {
		t.Fatalf("len(rows) = %d, want %d: %+v", got, want, m.rows)
	}
	if !m.rows[0].isHeader || m.rows[0].header != "Severity" {
		t.Errorf("rows[0] = %+v, want Severity header", m.rows[0])
	}
	if m.rows[1].isHeader || m.rows[1].row.severity != logging.Warning {
		t.Errorf("rows[1] = %+v, want Warning value row", m.rows[1])
	}
}

func TestFacetModelBuildRowsSkipsEmptyFields(t *testing.T) {
	var m facetModel
	m.setResult(gcplog.Facets{LogName: []gcplog.FacetCount{{Value: "x", Count: 1}}}, false)
	for _, ln := range m.rows {
		if ln.isHeader && ln.header != "Log Name" {
			t.Errorf("unexpected header %q for a field with no values", ln.header)
		}
	}
}

func TestFacetModelSelectionSkipsHeadersAndClamps(t *testing.T) {
	var m facetModel
	m.SetSize(40, 20)
	m.setResult(sampleFacets(), false)

	if m.rows[m.selected].isHeader {
		t.Fatalf("initial selection landed on a header: %+v", m.rows[m.selected])
	}
	start := m.selected

	m.moveSelection(1)
	if m.rows[m.selected].isHeader {
		t.Errorf("moveSelection(1) landed on a header: %+v", m.rows[m.selected])
	}
	if m.selected == start {
		t.Errorf("moveSelection(1) didn't move")
	}

	// Move down far past the end — must clamp at the last selectable row,
	// not wrap or go out of range.
	for range m.rows {
		m.moveSelection(1)
	}
	if m.selected < 0 || m.selected >= len(m.rows) || m.rows[m.selected].isHeader {
		t.Fatalf("selection out of range or on a header after clamping down: %d", m.selected)
	}
	lastSelected := m.selected
	m.moveSelection(1)
	if m.selected != lastSelected {
		t.Errorf("moveSelection(1) past the end moved further: %d -> %d", lastSelected, m.selected)
	}

	// And clamp at the top.
	for range m.rows {
		m.moveSelection(-1)
	}
	firstSelected := m.selected
	if m.rows[firstSelected].isHeader {
		t.Fatalf("selection landed on a header after clamping up: %d", firstSelected)
	}
	m.moveSelection(-1)
	if m.selected != firstSelected {
		t.Errorf("moveSelection(-1) past the start moved further: %d -> %d", firstSelected, m.selected)
	}
}

func TestFacetModelEnterEmitsSelectedRow(t *testing.T) {
	var m facetModel
	m.SetSize(40, 20)
	m.setResult(sampleFacets(), false)

	_, cmd := m.Update(textKey("enter"))
	if cmd == nil {
		t.Fatal("expected enter on a value row to emit a Cmd")
	}
	msg, ok := findInMsg[facetValueSelectedMsg](cmd())
	if !ok {
		t.Fatal("expected a facetValueSelectedMsg")
	}
	if msg.row != m.rows[m.selected].row {
		t.Errorf("emitted row = %+v, want %+v", msg.row, m.rows[m.selected].row)
	}
}

func TestFacetModelSetErrIsNonFatal(t *testing.T) {
	var m facetModel
	m.SetSize(40, 20)
	m.setErr(errors.New("boom"))
	if m.err == nil {
		t.Error("expected err to be recorded")
	}
	if m.loading {
		t.Error("setErr should clear loading")
	}
}
