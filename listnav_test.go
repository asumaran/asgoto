package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestListNavMoves(t *testing.T) {
	n := defaultListNav()
	press := func(code rune, mod tea.KeyMod) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: mod} }
	for _, tc := range []struct {
		name   string
		msg    tea.KeyPressMsg
		cursor int
		want   int
	}{
		{"down", press(tea.KeyDown, 0), 3, 4},
		{"up", press(tea.KeyUp, 0), 3, 2},
		{"ctrl+n", press('n', tea.ModCtrl), 3, 4},
		{"up at the top stays", press(tea.KeyUp, 0), 0, 0},
		{"page down", press(tea.KeyPgDown, 0), 3, 13},
		{"page down near the end stops at the last row", press(tea.KeyPgDown, 0), 45, 49},
		{"page up", press(tea.KeyPgUp, 0), 13, 3},
		{"home", press(tea.KeyHome, 0), 30, 0},
		{"end", press(tea.KeyEnd, 0), 30, 49},
		{"alt+up", press(tea.KeyUp, tea.ModAlt), 30, 0},
		{"alt+down", press(tea.KeyDown, tea.ModAlt), 30, 49},
	} {
		if !n.matches(tc.msg) {
			t.Errorf("%s: not a list key", tc.name)
		}
		if got := n.move(tc.msg, tc.cursor, 50, 10, nil); got != tc.want {
			t.Errorf("%s: cursor %d -> %d, want %d", tc.name, tc.cursor, got, tc.want)
		}
	}
	if n.matches(press('x', 0)) || n.matches(press(tea.KeyUp, tea.ModShift)) {
		t.Errorf("text and the preview's shift+arrows are not list keys")
	}
	if got := n.move(press(tea.KeyDown, 0), -1, 0, 10, nil); got != -1 {
		t.Errorf("an empty list leaves the cursor alone: %d", got)
	}
}

func TestListNavSkipsRowsTheCursorMayNotSitOn(t *testing.T) {
	n := defaultListNav()
	headers := map[int]bool{0: true, 4: true} // two groups: rows 1-3 and 5-7
	sel := func(i int) bool { return !headers[i] }
	down, up := tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyUp}
	if got := n.move(down, 3, 8, 5, sel); got != 5 {
		t.Errorf("down over a header: %d, want 5", got)
	}
	if got := n.move(up, 5, 8, 5, sel); got != 3 {
		t.Errorf("up over a header: %d, want 3", got)
	}
	if got := n.move(up, 1, 8, 5, sel); got != 1 {
		t.Errorf("up from the first row, with a header above: %d, want 1", got)
	}
	if got := n.move(tea.KeyPressMsg{Code: tea.KeyHome}, 6, 8, 5, sel); got != 1 {
		t.Errorf("home lands on the first row under the header: %d, want 1", got)
	}
	if got := n.move(tea.KeyPressMsg{Code: tea.KeyPgUp}, 6, 8, 2, sel); got != 3 {
		t.Errorf("a page that lands on a header carries on: %d, want 3", got)
	}
}
