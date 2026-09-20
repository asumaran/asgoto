package main

// The mouse over the list, the same in every tool of the family: the wheel
// moves the selection there (anywhere else it scrolls the preview), and a left
// click moves the cursor to the row under it. A click never opens anything:
// that stays on enter, so a stray click cannot switch a branch or a space.

import tea "charm.land/bubbletea/v2"

// inList reports whether the screen cell (x, y) is inside a list that starts
// on screen row top, one cell in from the frame's left side, width cells wide
// and height rows tall.
func inList(x, y, top, width, height int) bool {
	return x >= 1 && x <= width && y >= top && y < top+height
}

// rowUnder is the index of the list row on screen row y, for a list that
// starts on screen row top, is scrolled by offset and holds count rows.
func rowUnder(y, top, offset, count int) (int, bool) {
	i := y - top + offset
	return i, i >= 0 && i < count
}

// wheelKey is the key a wheel step over the list stands for, so the wheel
// goes through the same code as the arrows.
func wheelKey(msg tea.MouseWheelMsg) (tea.KeyPressMsg, bool) {
	switch msg.Button {
	case tea.MouseWheelUp:
		return tea.KeyPressMsg{Code: tea.KeyUp}, true
	case tea.MouseWheelDown:
		return tea.KeyPressMsg{Code: tea.KeyDown}, true
	}
	return tea.KeyPressMsg{}, false
}
