package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// renderFooter draws tailspin's one-line k9s-style hotkey menu for the
// current mode.
func (m appModel) renderFooter() string {
	var bindings []key.Binding
	switch m.mode {
	case modeError:
		bindings = []key.Binding{keys.Refresh, keys.Command, keys.Back, keys.Quit}
	case modeDetail:
		bindings = []key.Binding{keys.Up, keys.Down, keys.PageUp, keys.PageDown, keys.Back, keys.Quit}
	case modeTail:
		bindings = []key.Binding{pauseKey, keys.Up, keys.Down, keys.Back, keys.Quit}
	default:
		bindings = []key.Binding{keys.Up, keys.Down, pageNavKey, keys.Enter, keys.Filter, keys.Command, keys.Tail, keys.Wrap, keys.Refresh, keys.Help, keys.Quit}
	}

	var out strings.Builder
	for i, b := range bindings {
		h := b.Help()
		if i > 0 {
			out.WriteString("  ")
		}
		out.WriteString(footerKeyStyle.Render("<" + h.Key + ">"))
		out.WriteString(" ")
		out.WriteString(footerDescStyle.Render(h.Desc))
	}
	return out.String()
}
