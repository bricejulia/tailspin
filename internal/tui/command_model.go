package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// commandModel is the k9s-style ":" command prompt: a single-line input
// parsed on enter into a parsedCommand.
type commandModel struct {
	input textinput.Model
	err   error
}

func newCommandModel() commandModel {
	ti := textinput.New()
	ti.Prompt = ":"
	ti.SetWidth(60)
	return commandModel{input: ti}
}

// commandSubmittedMsg is emitted when the user presses enter on a
// successfully-parsed command.
type commandSubmittedMsg struct {
	cmd parsedCommand
}

func (m *commandModel) focus() tea.Cmd {
	m.err = nil
	m.input.Reset()
	return m.input.Focus()
}

func (m commandModel) Update(msg tea.Msg) (commandModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			parsed, err := parseCommand(m.input.Value())
			if err != nil {
				m.err = err
				return m, nil
			}
			m.err = nil
			return m, func() tea.Msg { return commandSubmittedMsg{cmd: parsed} }
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m commandModel) View() string {
	line := m.input.View()
	if m.err != nil {
		line += "  " + errorStyle.Render(m.err.Error())
	}
	return line
}
