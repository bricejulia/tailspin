package tui

import "strings"

// helpText renders modeHelp's body: a k9s-style cheat-sheet of hotkeys and
// ":" commands. This is a static placeholder — M6 replaces it with a proper
// bubbles/help overlay driven from keyMap, but :help/? need to show
// something real before then.
func helpText() string {
	lines := []string{
		headerStyle.Render("tailspin — keybindings"),
		"",
		footerKeyStyle.Render("j/k, ↓/↑") + "   move selection",
		footerKeyStyle.Render("pgup/pgdn") + "  scroll by a screenful",
		footerKeyStyle.Render("n / N") + "      next / previous page (fetches more once you're on the last loaded page)",
		footerKeyStyle.Render("enter") + "      view entry detail",
		footerKeyStyle.Render("/") + "          filter (severity, log name, text, time range)",
		footerKeyStyle.Render(":") + "          command mode",
		footerKeyStyle.Render("t") + "          tail (live streaming; space/p pauses, esc stops)",
		footerKeyStyle.Render("w") + "          toggle h-scroll: normally the focused row auto-wraps and others stay",
		"           compact; h-scroll instead keeps every row one line, h/l scrolls sideways",
		footerKeyStyle.Render("r") + "          refresh (re-run the current query from page 1)",
		footerKeyStyle.Render("esc") + "        back",
		footerKeyStyle.Render("q") + "          quit",
		"",
		headerStyle.Render(":") + " commands",
		"",
		footerKeyStyle.Render(":browse") + " (:b)         return to browse mode",
		footerKeyStyle.Render(":tail") + " (:t)           switch to tail mode",
		footerKeyStyle.Render(":project <id>") + " (:p)   switch GCP project",
		footerKeyStyle.Render(":help") + " (:h, :?)       this screen",
		footerKeyStyle.Render(":quit") + " (:q)           quit",
		"",
		statusStyle.Render("esc or q to close"),
	}
	return strings.Join(lines, "\n")
}
