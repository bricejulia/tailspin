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
	m.mode = modeQueries
	return m, nil
}

// queriesListText renders modeQueries' body, styled like help.go's helpText.
func (m appModel) queriesListText() string {
	lines := []string{
		headerStyle.Render("tailspin — saved queries"),
		"",
	}
	if len(m.savedQueries) == 0 {
		lines = append(lines, statusStyle.Render("no saved queries yet — \":save <name>\" saves the active filter"))
	} else {
		for _, q := range m.savedQueries {
			lines = append(lines, footerKeyStyle.Render(q.Name))
			if q.Description != "" {
				lines = append(lines, "  "+statusStyle.Render(q.Description))
			}
			for clause := range strings.SplitSeq(q.Filter, "\n") {
				lines = append(lines, "  "+headerMetaStyle.Render(clause))
			}
			lines = append(lines, "")
		}
	}
	lines = append(lines, statusStyle.Render("esc or q to close"))
	return strings.Join(lines, "\n")
}
