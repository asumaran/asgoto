package main

// The line at the foot of the frame, the same in every tool of the family.
// It has two shapes:
//
//   - without context: one line with the tool's own actions, the key that
//     opens the panel and the quit keys. bubbles' help lays it out
//     (ShortHelp); every other key is in the panel (panel.go), which the
//     same key opens.
//   - with context (which checkout and branch, which group, which
//     directory): the context on the left and the panel's key alone on the
//     right. The actions are in the panel; the context is what the rest of
//     the screen cannot say.
//
// A confirmation or a notice takes the context's place for a moment (the
// whole line when there is no context).
//
// The key is f1, the help key of htop, mc and nano. Never `?`: the filter has
// the focus, so a plain character is text. And never a symbol with ctrl:
// symbols move between keyboard layouts (ctrl+/ is ctrl+shift+7 on a Spanish
// one).
//
// This file is the same in every tool of the family.

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// stInfo is the context at the foot.
var stInfo = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

// helpBinding is the binding that opens the panel, named after what the tool
// has to offer there: its options, or the keys alone.
func helpBinding(hasOptions bool) key.Binding {
	desc := "help"
	if hasOptions {
		desc = "options"
	}
	return key.NewBinding(key.WithKeys("f1"), key.WithHelp("f1", desc))
}

// isHelpKey reports whether msg opens the panel.
func isHelpKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, helpBinding(false))
}

// helpLine is the help cut to width. bubbles' help keeps appending items past
// its width when the ellipsis does not fit, hence the cut.
func helpLine(h help.Model, keys help.KeyMap, width int) string {
	h.ShowAll = false
	line, _, _ := strings.Cut(h.View(keys), "\n")
	return ansi.Truncate(line, max(0, width), "…")
}

// panelHint is the panel's key alone ("f1 options"), laid out by bubbles'
// help like the rest of the line, taken from the tool's own ShortHelp so the
// description is the tool's. Empty when ShortHelp offers no f1.
func panelHint(h help.Model, keys help.KeyMap) string {
	for _, b := range keys.ShortHelp() {
		if b.Enabled() && slices.Contains(b.Keys(), "f1") {
			h.SetWidth(0) // whatever the model's width, the hint is never cut
			return h.ShortHelpView([]key.Binding{b})
		}
	}
	return ""
}

// footRoom is the width the context has at the foot: the line minus the
// panel's key and the gap before it, one cell at least. The context is fitted
// to it before it is drawn (a path loses its head, not the branch its place).
func footRoom(h help.Model, keys help.KeyMap, width int) int {
	return max(1, width-ansi.StringWidth(panelHint(h, keys))-2)
}

// footLine is the line at the foot. Without context (info empty): a
// confirmation while one is flashing, else a notice (an error, in its color,
// until the next key), else the help. With context (info, already styled and
// fitted to footRoom): the same on the left, the panel's key on the right.
// The line never exceeds width; below the hint's own width the hint is cut.
func footLine(f flash, notice, info string, h help.Model, keys help.KeyMap, width int) string {
	width = max(0, width)
	hint := panelHint(h, keys)
	if info == "" || hint == "" {
		switch {
		case f.text != "":
			return f.view(width)
		case notice != "":
			return stError.Render(truncate(notice, width))
		}
		return helpLine(h, keys, width)
	}
	room := footRoom(h, keys, width)
	left := ansi.Truncate(info, room, "…")
	switch {
	case f.text != "":
		left = f.view(room)
	case notice != "":
		left = stError.Render(truncate(notice, room))
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(hint)
	if gap < 1 {
		return ansi.Truncate(left+" "+hint, width, "")
	}
	return left + strings.Repeat(" ", gap) + hint
}
