package main

// The key help at the foot of the frame, the same in every tool of the
// family: one line with the tool's own actions, the key that opens the panel
// and the quit keys. bubbles' help lays it out (ShortHelp); every other key
// is in the panel (panel.go), which the same key opens.
//
// The key is f1, the help key of htop, mc and nano. Never `?`: the filter has
// the focus, so a plain character is text. And never a symbol with ctrl:
// symbols move between keyboard layouts (ctrl+/ is ctrl+shift+7 on a Spanish
// one).
//
// This file is the same in every tool of the family.

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

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
