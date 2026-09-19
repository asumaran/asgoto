package main

// The keys that move the cursor through a list, the same in every tool of the
// family: a row, a page, the ends. home/end would otherwise move the caret of
// the filter input, which left/right and ctrl+e already do; the list needs
// them more. Laptop keyboards have no home/end (fn+arrows sends them, when
// the terminal lets it through), so the ends are also on alt+arrows. The
// preview scrolls with shift+arrows, never with pgup/pgdn: those page the
// list.
//
// This file is the same in every tool of the family.

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type listNav struct {
	Up, Down, PageUp, PageDown, Top, Bottom key.Binding
}

func defaultListNav() listNav {
	return listNav{
		Up:       key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/↓", "move")),
		Down:     key.NewBinding(key.WithKeys("down", "ctrl+n")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup/pgdn", "move a page")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Top:      key.NewBinding(key.WithKeys("alt+up", "home", "ctrl+home"), key.WithHelp("⌥↑/⌥↓", "top/bottom")),
		Bottom:   key.NewBinding(key.WithKeys("alt+down", "end", "ctrl+end")),
	}
}

// matches reports whether msg is one of the list's keys.
func (n listNav) matches(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, n.Up, n.Down, n.PageUp, n.PageDown, n.Top, n.Bottom)
}

// move returns where msg takes the cursor in a list of count rows that shows
// page of them at a time. selectable tells the rows the cursor may sit on
// from the rest (group headers); nil means every row. A move that lands on a
// row it may not sit on carries on in the same direction, then falls back the
// other way; with nowhere to go the cursor stays.
func (n listNav) move(msg tea.KeyPressMsg, cursor, count, page int, selectable func(int) bool) int {
	if count <= 0 {
		return cursor
	}
	page = max(1, page)
	d := 0
	switch {
	case key.Matches(msg, n.Up):
		d = -1
	case key.Matches(msg, n.Down):
		d = 1
	case key.Matches(msg, n.PageUp):
		d = -page
	case key.Matches(msg, n.PageDown):
		d = page
	case key.Matches(msg, n.Top):
		d = -count
	case key.Matches(msg, n.Bottom):
		d = count
	default:
		return cursor
	}
	to := min(max(cursor+d, 0), count-1)
	ok := func(i int) bool { return selectable == nil || selectable(i) }
	step := 1
	if d < 0 {
		step = -1
	}
	for i := to; i >= 0 && i < count; i += step {
		if ok(i) {
			return i
		}
	}
	for i := to - step; i >= 0 && i < count && i != cursor; i -= step {
		if ok(i) {
			return i
		}
	}
	return cursor
}
