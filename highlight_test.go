package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHighlightMarksMatchesOverTheBaseStyle(t *testing.T) {
	if !stMatch.GetUnderline() || matchOver(stSel).GetBackground() != stSel.GetBackground() || !matchOver(stSel).GetBold() {
		t.Errorf("a match is underlined and keeps the style it sits on")
	}
	plain := lipgloss.NewStyle()
	for _, base := range []lipgloss.Style{plain, stSel} {
		got := highlight("fix cart", []int{0, 1, 2}, base)
		if !strings.Contains(got, matchOver(base).Render("fix")) || !strings.HasSuffix(got, base.Render(" cart")) {
			t.Errorf("got %q, want the match as one run and the rest in base", got)
		}
		if ansi.Strip(got) != "fix cart" {
			t.Errorf("highlighting must not change the text: %q", ansi.Strip(got))
		}
	}
	if got := highlight("fix cart", nil, stSel); got != stSel.Render("fix cart") {
		t.Errorf("without matches the text is just base: %q", got)
	}
	if highlight("", []int{0}, stSel) != "" {
		t.Errorf("nothing to render, nothing rendered")
	}
}

func TestHighlightReadsByteOffsets(t *testing.T) {
	s := "Añadir cabeceras" // ñ is two bytes: "cab" starts at byte 8
	got := highlight(s, []int{8, 9, 10}, lipgloss.NewStyle())
	if !strings.Contains(got, stMatch.Render("cab")) || ansi.Strip(got) != s {
		t.Errorf("got %q, want cab marked", got)
	}
	// a piece of a longer text: the offsets still refer to the whole
	piece := highlightFrom(s[8:], 8, matchSet([]int{8, 9, 10}), lipgloss.NewStyle())
	if !strings.HasPrefix(piece, stMatch.Render("cab")) || ansi.Strip(piece) != "cabeceras" {
		t.Errorf("piece = %q", piece)
	}
}

func TestSelPadFillsTheRowWithTheSelection(t *testing.T) {
	got := selPad(stSel.Render("ab"), 5)
	if ansi.StringWidth(got) != 5 || !strings.HasSuffix(got, stSel.Render("   ")) {
		t.Errorf("got %q", got)
	}
	if selPad(stSel.Render("abcdef"), 3) != stSel.Render("abcdef") {
		t.Errorf("a piece that is already wide enough is left alone")
	}
	if onSel(stMatch).GetBackground() != stSel.GetBackground() || onSel(stMatch).GetForeground() != stMatch.GetForeground() {
		t.Errorf("onSel keeps the colors and adds the selection")
	}
}
