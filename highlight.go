package main

// How a filter match and the selected row look, for every tool of the family:
// a match is the match color plus an underline on top of whatever style the
// text already has (a bold title, a dim path, the selected row's background),
// so it stays visible where the cursor is.
//
// The selected row is never one big stSel.Render around styled text: the
// reset that ends a match would cut the background. Every piece of the row is
// rendered with the background itself, which is what highlight does when its
// base is stSel, and selPad fills what is left.
//
// This file is the same in every tool of the family.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	stSel   = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	stMatch = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Underline(true)
)

// onSel is base as it looks on the selected row: its own colors over the
// selection's background.
func onSel(base lipgloss.Style) lipgloss.Style {
	return base.Background(stSel.GetBackground()).Bold(true)
}

// matchOver is the match style on top of base.
func matchOver(base lipgloss.Style) lipgloss.Style {
	return base.Foreground(stMatch.GetForeground()).Underline(true)
}

// matchSet indexes the matcher's byte offsets.
func matchSet(idx []int) map[int]bool {
	if len(idx) == 0 {
		return nil
	}
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		set[i] = true
	}
	return set
}

// highlight renders s in base with the bytes at idx marked as matches.
func highlight(s string, idx []int, base lipgloss.Style) string {
	return highlightFrom(s, 0, matchSet(idx), base)
}

// highlightFrom is highlight for a piece of a longer text: s starts at byte
// off of the text the offsets in matched refer to.
func highlightFrom(s string, off int, matched map[int]bool, base lipgloss.Style) string {
	if s == "" {
		return ""
	}
	if len(matched) == 0 {
		return base.Render(s)
	}
	match := matchOver(base)
	var b strings.Builder
	start, on := 0, false
	flush := func(end int) {
		if end > start {
			if on {
				b.WriteString(match.Render(s[start:end]))
			} else {
				b.WriteString(base.Render(s[start:end]))
			}
		}
		start = end
	}
	for i := range s { // i is a byte offset, like the matcher's
		if m := matched[off+i]; m != on {
			flush(i)
			on = m
		}
	}
	flush(len(s))
	return b.String()
}

// selPad pads an already styled piece of the selected row to width, so the
// row's background has no gaps.
func selPad(s string, width int) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		s += stSel.Render(strings.Repeat(" ", n))
	}
	return s
}
