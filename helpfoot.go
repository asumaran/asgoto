package main

// The key help at the foot of the frame, the same in every tool of the
// family: one line with the tool's own actions, or every key in columns while
// `?` has it expanded, and the main section gives way. bubbles' help does the
// layout (ShortHelp, and one column per FullHelp group); this file decides
// the key, the height and the cut.
//
// `?` expands only while the filter is empty, otherwise it is text, like q;
// f1 always does. esc folds the help before it quits.
//
// This file is the same in every tool of the family.

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// helpKey is the binding the help itself shows. `?` is matched by isHelpKey,
// not here, because it depends on the filter.
var helpKey = key.NewBinding(key.WithKeys("f1"), key.WithHelp("?", "help"))

// isHelpKey reports whether msg expands or folds the help.
func isHelpKey(msg tea.KeyPressMsg, filter string) bool {
	return key.Matches(msg, helpKey) || msg.String() == "?" && filter == ""
}

// foldsHelp reports whether msg is the esc that folds an expanded help
// instead of quitting.
func foldsHelp(msg tea.KeyPressMsg, h help.Model) bool {
	return h.ShowAll && msg.String() == "esc"
}

// helpHeight is how many lines the help takes: one, or the full help while it
// is expanded, never more than room so the main section is not squeezed out.
func helpHeight(h help.Model, keys help.KeyMap, room int) int {
	if !h.ShowAll {
		return 1
	}
	return max(1, min(lipgloss.Height(h.View(keys)), room))
}

// helpLines is the help as height lines cut to width. bubbles' help keeps
// appending items past its width when the ellipsis does not fit, hence the
// cut.
func helpLines(h help.Model, keys help.KeyMap, width, height int) []string {
	lines := strings.Split(h.View(keys), "\n")
	lines = lines[:min(len(lines), max(1, height))]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, max(0, width), "…")
	}
	return lines
}
