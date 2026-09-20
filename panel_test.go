package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func panelOpts() []option {
	return []option{
		{id: "renderer", label: "Diff renderer", values: []string{"delta", "hunk"}, cur: 1},
		{id: "diff", label: "Diff mode", values: []string{"auto", "side by side", "single"}, key: "^t"},
	}
}

func pk(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "f1":
		return tea.KeyPressMsg{Code: tea.KeyF1}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestPanelKeys(t *testing.T) {
	opts := panelOpts()
	p := panel{}
	p.toggle()
	if !p.open || p.cursor != 0 {
		t.Fatalf("toggle should open the panel on the first option: %+v", p)
	}
	if a := p.update(pk("up"), opts); p.cursor != 0 || a.id != "" {
		t.Errorf("up at the top stays put: %+v %+v", p, a)
	}
	p.update(pk("down"), opts)
	p.update(pk("down"), opts)
	if p.cursor != 1 {
		t.Errorf("down stops at the last option, cursor %d", p.cursor)
	}
	if a := p.update(pk("left"), opts); a.id != "diff" || a.value != 2 {
		t.Errorf("left goes around to the last value: %+v", a)
	}
	for _, k := range []string{"right", "space", "enter"} {
		if a := p.update(pk(k), opts); a.id != "diff" || a.value != 1 {
			t.Errorf("%s asks for the next value: %+v", k, a)
		}
	}
	if a := p.update(pk("x"), opts); a.id != "" || !p.open {
		t.Errorf("any other key is swallowed: %+v open=%v", a, p.open)
	}
	for _, k := range []string{"esc", "f1", "?", "q"} {
		p.open = true
		if a := p.update(pk(k), opts); p.open || a.id != "" {
			t.Errorf("%s closes the panel and changes nothing: %+v", k, a)
		}
	}
	// No options: nothing to move over or to set.
	p = panel{open: true}
	if a := p.update(pk("enter"), nil); a.id != "" || p.cursor != 0 {
		t.Errorf("enter without options does nothing: %+v %+v", a, p)
	}
}

func TestPanelLines(t *testing.T) {
	keys := keyLines(help.New(), footKeys{}, 60)
	box := panelLines(panelOpts(), 0, keys, 80, 20)
	plain := make([]string, len(box))
	for i, l := range box {
		plain[i] = ansi.Strip(l)
		if ansi.StringWidth(l) != ansi.StringWidth(box[0]) {
			t.Errorf("line %d is %d cells wide, the box is %d", i, ansi.StringWidth(l), ansi.StringWidth(box[0]))
		}
	}
	all := strings.Join(plain, "\n")
	for _, want := range []string{"╭─ options ", "Options", "▌ Diff renderer   delta  ‹hunk›", "‹auto›  side by side   single    ^t", "Keys", "three", "esc close"} {
		if !strings.Contains(all, want) {
			t.Errorf("the panel lacks %q:\n%s", want, all)
		}
	}
	// Short of rows the keys go first; the options and the hint stay.
	small := ansi.Strip(strings.Join(panelLines(panelOpts(), 1, keys, 80, 9), "\n"))
	if strings.Contains(small, "three") || !strings.Contains(small, "▌ Diff mode") || !strings.Contains(small, "esc close") {
		t.Errorf("a short panel keeps the options and the hint:\n%s", small)
	}
	if got := len(panelLines(panelOpts(), 0, keys, 80, 9)); got > 9 {
		t.Errorf("the panel is %d lines, want 9 at most", got)
	}
	for _, l := range panelLines(panelOpts(), 0, keys, 30, 20) {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("line %q is wider than 30", ansi.Strip(l))
		}
	}
	// Without options there is no such section.
	bare := ansi.Strip(strings.Join(panelLines(nil, 0, keys, 80, 20), "\n"))
	if !strings.Contains(bare, "╭─ help ") || strings.Contains(bare, "Options") || strings.Contains(bare, "change") || !strings.Contains(bare, "Keys") {
		t.Errorf("a tool without options shows the keys alone:\n%s", bare)
	}
}

func TestOverlayKeepsTheFrame(t *testing.T) {
	const w = 40
	frame := make([]string, 10)
	for i := range frame {
		frame[i] = stDim.Render("│") + stPanelOn.Render(strings.Repeat("x", w-2)) + stDim.Render("│")
	}
	box := []string{hline(12, "╭", "╮", "", ""), framed(12, "hi"), hline(12, "╰", "╯", "", "")}
	out := overlay(frame, box, w)
	if len(out) != len(frame) {
		t.Fatalf("overlay changed the line count: %d -> %d", len(frame), len(out))
	}
	for i, l := range out {
		if ansi.StringWidth(l) != w {
			t.Errorf("line %d is %d cells wide, want %d", i, ansi.StringWidth(l), w)
		}
	}
	mid := ansi.Strip(out[4])
	if !strings.HasPrefix(mid, "│"+strings.Repeat("x", 13)+"│ hi") || !strings.HasSuffix(mid, strings.Repeat("x", 13)+"│") {
		t.Errorf("the box should sit in the middle of the line: %q", mid)
	}
	if ansi.Strip(out[0]) != ansi.Strip(frame[0]) || ansi.Strip(out[9]) != ansi.Strip(frame[9]) {
		t.Errorf("lines outside the box should not change")
	}
	// A frame line shorter than the box's column is padded up to it.
	short := overlay([]string{"", "ab", ""}, []string{"[]"}, 10)
	if short[1] != "ab  \x1b[0m[]\x1b[0m" {
		t.Errorf("short line = %q", short[1])
	}
	// A box that does not fit leaves the frame alone.
	if got := overlay(frame[:2], box, w); len(got) != 2 || got[0] != frame[0] {
		t.Errorf("a box taller than the frame should be dropped")
	}
}
