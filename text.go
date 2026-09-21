package main

// Fitting text into cells. This file is the same in every tool of the family.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// truncate cuts s to width cells, ending in "…" when it was cut. s may carry
// ANSI styling.
func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// padRight cuts or pads s to exactly width cells.
func padRight(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// padLeft right-aligns s in width cells.
func padLeft(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s = strings.Repeat(" ", n) + s
	}
	return s
}

// firstLine is the first line of s, without the surrounding space.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// plural is n and its noun, with an s unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// errorBlock is an error for a preview, in the error color: every line of it
// (git says what went wrong in several) cut to width, none of them wrapped.
func errorBlock(msg string, width int) string {
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	for i, l := range lines {
		lines[i] = stError.Render(truncate(l, max(0, width)))
	}
	return strings.Join(lines, "\n")
}
