package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/config"
)

// saveCurrentQuery persists the active filter's reusable, non-time-bound
// part (see gcplog.FilterState.Query) under name. Local file I/O is
// synchronous here, not a tea.Cmd: it's one small JSON file, with no
// user-perceptible latency to hide behind a spinner, unlike the network
// calls tea.Cmd exists for elsewhere in this app.
func (m appModel) saveCurrentQuery(name string) (appModel, tea.Cmd) {
	path, err := config.QueriesPath()
	if err != nil {
		m.notice = "save failed: " + err.Error()
		return m, nil
	}
	if err := config.SaveQuery(path, config.SavedQuery{Name: name, Filter: m.filter.Query()}); err != nil {
		m.notice = "save failed: " + err.Error()
		return m, nil
	}
	m.notice = "saved query " + name
	m.mode = modeBrowse
	return m, nil
}

// loadSavedQuery loads the saved query named name as the active RawQuery
// (via applyRawQuery, so the Since invariant is enforced the same way a
// ":query" submission enforces it) and refetches.
func (m appModel) loadSavedQuery(name string) (appModel, tea.Cmd) {
	path, err := config.QueriesPath()
	if err != nil {
		m.notice = "load failed: " + err.Error()
		return m, nil
	}
	queries, err := config.LoadQueries(path)
	if err != nil {
		m.notice = "load failed: " + err.Error()
		return m, nil
	}
	q, ok := config.FindQuery(queries, name)
	if !ok {
		m.notice = "no saved query named " + name
		return m, nil
	}
	return m.applyRawQuery(q.Filter)
}

// showSavedQueries loads the saved queries file and switches to modeQueries
// to display it.
func (m appModel) showSavedQueries() (appModel, tea.Cmd) {
	path, err := config.QueriesPath()
	if err != nil {
		m.notice = "listing queries failed: " + err.Error()
		return m, nil
	}
	queries, err := config.LoadQueries(path)
	if err != nil {
		m.notice = "listing queries failed: " + err.Error()
		return m, nil
	}
	m.savedQueries = queries
	m.queriesSelected = 0
	m.mode = modeQueries
	return m, nil
}

// moveQueriesSelection clamps the selected saved-query index by delta —
// same shape as listModel.moveSelection, just simple enough (no scrolling
// needed) not to warrant its own sub-model.
func (m *appModel) moveQueriesSelection(delta int) {
	if len(m.savedQueries) == 0 {
		return
	}
	m.queriesSelected += delta
	if m.queriesSelected < 0 {
		m.queriesSelected = 0
	}
	if m.queriesSelected >= len(m.savedQueries) {
		m.queriesSelected = len(m.savedQueries) - 1
	}
}

// runSelectedQuery loads the currently-highlighted saved query, the same
// way :load <name> does.
func (m appModel) runSelectedQuery() (appModel, tea.Cmd) {
	if m.queriesSelected < 0 || m.queriesSelected >= len(m.savedQueries) {
		return m, nil
	}
	return m.applyRawQuery(m.savedQueries[m.queriesSelected].Filter)
}

// queriesListText renders modeQueries' body, styled like help.go's helpText.
// The highlighted entry (see appModel.queriesSelected) uses the same
// full-line background + marker convention as the browse list's selected
// row, so "which one will enter run" is unambiguous.
func (m appModel) queriesListText() string {
	lines := []string{
		headerStyle.Render("tailspin — saved queries"),
		"",
	}
	if len(m.savedQueries) == 0 {
		lines = append(lines, statusStyle.Render("no saved queries yet — \":save <name>\" saves the active filter"))
	} else {
		for i, q := range m.savedQueries {
			if i == m.queriesSelected {
				lines = append(lines, selectedRowStyle.Render(padToWidth("> "+q.Name, m.width)))
			} else {
				lines = append(lines, "  "+footerKeyStyle.Render(q.Name))
			}
			if q.Description != "" {
				lines = append(lines, "    "+statusStyle.Render(q.Description))
			}
			for clause := range strings.SplitSeq(q.Filter, "\n") {
				lines = append(lines, "    "+headerMetaStyle.Render(clause))
			}
			lines = append(lines, "")
		}
	}
	lines = append(lines, statusStyle.Render("j/k select · enter run · esc/q close"))
	return strings.Join(lines, "\n")
}
