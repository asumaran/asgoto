package main

// The panel f1 opens over the frame: the tool's options, to change in
// place, and every key under them. A setting that is chosen once lives here
// instead of taking a key of its own; the ones changed all the time keep
// their key, and the panel names it. The frame behind does not move: the
// panel is spliced over its lines, cell for cell.
//
// A tool lists its options (options) and applies one (setOption); the panel
// owns the cursor, the keys and the drawing. While it is open it takes every
// key, so nothing reaches the filter.
//
// This file is the same in every tool of the family.

import (
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	stPanelHead = lipgloss.NewStyle().Bold(true)
	stPanelOn   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	stPanelMark = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

// option is one setting: its values in the order they cycle, and which one
// is on. key is the direct shortcut, "" when the panel is the only way.
type option struct {
	id     string
	label  string
	values []string
	cur    int
	key    string
}

// step is the value dir places from the current one, around the ends.
func (o option) step(dir int) int {
	n := len(o.values)
	if n == 0 {
		return 0
	}
	return ((o.cur+dir)%n + n) % n
}

type panel struct {
	open   bool
	cursor int
}

// panelAction is what a key asked for: set the option id to value, or nothing
// (id is empty) when the key moved the cursor, closed the panel or was
// swallowed.
type panelAction struct {
	id    string
	value int
}

func (p *panel) toggle() {
	p.open = !p.open
	p.cursor = 0
}

// update handles a key while the panel is open.
func (p *panel) update(msg tea.KeyPressMsg, opts []option) panelAction {
	p.cursor = max(0, min(p.cursor, len(opts)-1))
	dir := 0
	switch msg.String() {
	case "esc", "f1", "?", "q":
		p.open = false
	case "up", "ctrl+p":
		p.cursor = max(0, p.cursor-1)
	case "down", "ctrl+n":
		p.cursor = max(0, min(p.cursor+1, len(opts)-1))
	case "left":
		dir = -1
	case "right", "space", "enter":
		dir = 1
	}
	if dir == 0 || len(opts) == 0 {
		return panelAction{}
	}
	o := opts[p.cursor]
	return panelAction{id: o.id, value: o.step(dir)}
}

// optionLines draws the options in columns: cursor, label, values with the
// current one marked, and the direct key at the right.
func optionLines(opts []option, cursor int) []string {
	labelW := 0
	for _, o := range opts {
		labelW = max(labelW, ansi.StringWidth(o.label))
	}
	left := make([]string, len(opts))
	leftW := 0
	for i, o := range opts {
		mark := "  "
		if i == cursor {
			mark = stPanelMark.Render("▌") + " "
		}
		vals := make([]string, len(o.values))
		for j, v := range o.values {
			if j == o.cur {
				vals[j] = stPanelOn.Render("‹" + v + "›")
			} else {
				vals[j] = stDim.Render(" " + v + " ")
			}
		}
		left[i] = mark + fit(o.label, labelW) + "  " + strings.Join(vals, " ")
		leftW = max(leftW, ansi.StringWidth(left[i]))
	}
	for i, o := range opts {
		if o.key != "" {
			left[i] = fit(left[i], leftW) + "   " + stDim.Render(o.key)
		}
	}
	return left
}

// keyLines is every key of a key map in columns, as bubbles' help lays out
// its full view, cut to width.
func keyLines(h help.Model, keys help.KeyMap, width int) []string {
	h.ShowAll = true
	h.SetWidth(max(0, width))
	lines := strings.Split(h.View(keys), "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, max(0, width), "…")
	}
	return lines
}

// panelLines is the panel as a box no larger than maxW by maxH, titled after
// what it holds. Short of
// rows, the keys are cut first: the options and the closing hint stay.
func panelLines(opts []option, cursor int, keys []string, maxW, maxH int) []string {
	// The title is what the help line calls the panel (helpBinding).
	title, hint := "help", "esc close"
	var head []string
	if len(opts) > 0 {
		title = "options"
		hint = "↑/↓ move • ←/→ or space change • esc close"
		head = append(head, stPanelHead.Render("Options"))
		head = append(head, optionLines(opts, max(0, min(cursor, len(opts)-1)))...)
		head = append(head, "")
	}
	head = append(head, stPanelHead.Render("Keys"))
	tail := []string{"", stDim.Render(hint)}

	room := max(0, maxH-2-len(head)-len(tail))
	body := head
	for _, k := range keys[:min(len(keys), room)] {
		body = append(body, "  "+k) // under the labels of the options
	}
	body = append(body, tail...)
	body = body[:min(len(body), max(0, maxH-2))]

	inner := 0
	for _, l := range body {
		inner = max(inner, ansi.StringWidth(l))
	}
	w := min(inner+4, maxW) // the sides and a cell of padding on each
	out := []string{hline(w, "╭", "╮", title, "")}
	for _, l := range body {
		out = append(out, framed(w, fit(l, max(0, w-4))))
	}
	return append(out, hline(w, "╰", "╯", "", ""))
}

// overlay sets box over the middle of frame. The line count does not change,
// and a frame line that was width cells wide still is.
func overlay(frame, box []string, width int) []string {
	if len(box) == 0 || len(box) > len(frame) {
		return frame
	}
	boxW := 0
	for _, l := range box {
		boxW = max(boxW, ansi.StringWidth(l))
	}
	x := max(0, (width-boxW)/2)
	y := (len(frame) - len(box)) / 2
	out := append([]string(nil), frame...)
	for i, b := range box {
		row := frame[y+i]
		left := ansi.Truncate(row, x, "")
		left += strings.Repeat(" ", max(0, x-ansi.StringWidth(left)))
		right := ""
		if rest := ansi.StringWidth(row) - x - boxW; rest > 0 {
			right = ansi.TruncateLeft(row, x+boxW, "")
			right = strings.Repeat(" ", max(0, rest-ansi.StringWidth(right))) + right
		}
		out[y+i] = left + "\x1b[0m" + fit(b, boxW) + "\x1b[0m" + right
	}
	return out
}

// nextValue is the value after the current one of the option id: what a key
// that cycles a setting directly asks setOption for.
func nextValue(opts []option, id string) int {
	for _, o := range opts {
		if o.id == id {
			return o.step(1)
		}
	}
	return 0
}
