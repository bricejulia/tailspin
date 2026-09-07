package tui

import "charm.land/bubbles/v2/key"

// keyMap is the single source of truth for tailspin's keybindings. Sub-models
// reuse these bindings rather than hand-coding key strings, so the footer
// hints and the actual handling never drift apart.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	NextPage key.Binding
	PrevPage key.Binding
	Enter    key.Binding
	Back     key.Binding
	Filter   key.Binding
	Command  key.Binding
	Tail     key.Binding
	Facets   key.Binding
	Wrap     key.Binding
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// tabKey and shiftTabKey move focus between fields in the filter bar. They
// aren't part of keyMap (and so don't show up in the footer) since they're
// only meaningful while modeFilterFocus is active.
var (
	tabKey      = key.NewBinding(key.WithKeys("tab"))
	shiftTabKey = key.NewBinding(key.WithKeys("shift+tab"))
)

// pageNavKey is a footer-only display binding combining NextPage/PrevPage
// into one compact "n/N" hint — matching is still done against the two
// separate bindings in keyMap.
var pageNavKey = key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n/N", "next/prev page"))

// runQueryKey is a footer-only display binding for modeQueries: same key
// as keys.Enter, but "run" reads better than keys.Enter's "view" there.
// Matching for modeQueries' own enter handling is done by literal string
// comparison in handleKey, not against this binding.
var runQueryKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run"))

// facetApplyKey is modeFacetFocus's footer-only display binding: same key
// as keys.Enter, but "apply" reads better than keys.Enter's "view" there.
// Matching is done by facetModel.Update against a literal "enter" string
// comparison, not against this binding, the same split runQueryKey uses.
var facetApplyKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply"))

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "ctrl+b"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "ctrl+f"),
		key.WithHelp("pgdn", "page down"),
	),
	NextPage: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next page"),
	),
	PrevPage: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "prev page"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "view"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Command: key.NewBinding(
		key.WithKeys(":"),
		key.WithHelp(":", "command"),
	),
	Tail: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "tail"),
	),
	Facets: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "facets"),
	),
	Wrap: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "wrap"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
