package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// querySubmitKey commits the raw query editor's content. Not part of
// keyMap (so it doesn't show up outside modeQuery's own footer) since it's
// only meaningful there — like tail_model.go's pauseKey. Enter/ctrl+m are
// textarea's own InsertNewline binding (a multi-line editor can't use
// Enter to submit), and ctrl+s isn't bound to anything in
// textarea.DefaultKeyMap(), so there's no collision.
var querySubmitKey = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "run query"))

// queryModel is the ":query"-triggered raw/advanced filter editor: a full
// multi-line text area, since Cloud Logging's filter language treats
// newline-separated clauses as implicitly ANDed (the same shape Cloud
// Console's own query builder produces) — textinput can't be used here,
// it collapses newlines to spaces on input.
type queryModel struct {
	textarea textarea.Model
}

func newQueryModel() queryModel {
	ta := textarea.New()
	ta.Placeholder = "resource.type=\"k8s_container\"\nresource.labels.cluster_name=\"my-cluster\""
	ta.ShowLineNumbers = false
	return queryModel{textarea: ta}
}

func (m *queryModel) SetSize(width, height int) {
	m.textarea.SetWidth(width)
	m.textarea.SetHeight(height)
}

// seed primes the editor from the currently-active raw query (empty if
// the active filter is currently structured, not raw — this doesn't
// attempt to convert the structured fields into raw text).
func (m *queryModel) seed(raw string) {
	m.textarea.SetValue(raw)
}

func (m *queryModel) focus() tea.Cmd {
	return m.textarea.Focus()
}

// querySubmittedMsg is emitted when the user commits the raw query editor.
type querySubmittedMsg struct {
	rawQuery string
}

func (m queryModel) Update(msg tea.Msg) (queryModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && key.Matches(keyMsg, querySubmitKey) {
		value := m.textarea.Value()
		return m, func() tea.Msg { return querySubmittedMsg{rawQuery: value} }
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m queryModel) View() string {
	return m.textarea.View()
}
