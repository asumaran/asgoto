package main

// The primitives the frame is drawn with: an edge with texts set into it, a
// line between the frame's sides, and the position a scrolled viewport
// reports on an edge.
//
// This file is the same in every tool of the family.

import (
	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"strconv"
	"strings"
)

// hline draws a horizontal border w cells wide between the corners l and r
// (either may be empty), with optional (already styled) texts set into it
// near each end.
func hline(w int, l, r, left, right string) string {
	inner := max(0, w-ansi.StringWidth(l)-ansi.StringWidth(r))
	if left != "" {
		left = " " + left + " "
	}
	if right != "" {
		right = " " + right + " "
	}
	if 2+ansi.StringWidth(left)+ansi.StringWidth(right) > inner {
		right = ""
	}
	if 1+ansi.StringWidth(left) > inner {
		left = ansi.Truncate(left, max(0, inner-1), "")
	}
	fill := inner - ansi.StringWidth(left) - ansi.StringWidth(right)
	lead := min(1, fill)
	tail := 0
	if right != "" {
		tail = min(1, fill-lead)
	}
	return stDim.Render(l+strings.Repeat("─", lead)) + left +
		stDim.Render(strings.Repeat("─", fill-lead-tail)) + right + stDim.Render(strings.Repeat("─", tail)+r)
}

// fit truncates or pads s to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// framed sets a line, with a cell of padding, between the frame's sides.
func framed(w int, l string) string {
	side := stDim.Render("│")
	return side + fit(" "+l, w-2) + side
}

// scrollPos is the preview's position for the main section's bottom edge,
// empty while everything fits.
func scrollPos(vp *viewport.Model) string {
	total := vp.TotalLineCount()
	if total <= vp.Height() {
		return ""
	}
	return stDim.Render(strconv.Itoa(min(total, vp.YOffset()+vp.Height())) + "/" + strconv.Itoa(total))
}
