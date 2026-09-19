package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type footKeys struct{}

func (footKeys) ShortHelp() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")), helpKey}
}

func (footKeys) FullHelp() [][]key.Binding {
	b := func(k, d string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, d)) }
	return [][]key.Binding{{b("a", "one"), b("b", "two"), b("c", "three")}, {b("d", "four"), helpKey}}
}

func TestHelpKeyDependsOnTheFilter(t *testing.T) {
	q, f1 := tea.KeyPressMsg{Code: '?', Text: "?"}, tea.KeyPressMsg{Code: tea.KeyF1}
	if !isHelpKey(q, "") || isHelpKey(q, "fix") {
		t.Errorf("? expands the help only while the filter is empty")
	}
	if !isHelpKey(f1, "") || !isHelpKey(f1, "fix") {
		t.Errorf("f1 expands the help whatever the filter says")
	}
	h := help.New()
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	if foldsHelp(esc, h) {
		t.Errorf("esc quits while the help is folded")
	}
	h.ShowAll = true
	if !foldsHelp(esc, h) || foldsHelp(q, h) {
		t.Errorf("esc, and only esc, folds an expanded help")
	}
}

func TestHelpHeightAndLines(t *testing.T) {
	h := help.New()
	h.SetWidth(80)
	if got := helpHeight(h, footKeys{}, 10); got != 1 {
		t.Errorf("folded height = %d, want 1", got)
	}
	short := helpLines(h, footKeys{}, 80, 1)
	if len(short) != 1 || !strings.Contains(ansi.Strip(short[0]), "? help") {
		t.Errorf("folded = %q, want one line that offers the help", short)
	}
	h.ShowAll = true
	if got := helpHeight(h, footKeys{}, 10); got != 3 {
		t.Errorf("expanded height = %d, want the 3 rows of the tallest column", got)
	}
	if got := helpHeight(h, footKeys{}, 2); got != 2 {
		t.Errorf("height = %d, want it capped by the room left", got)
	}
	full := helpLines(h, footKeys{}, 80, 3)
	if len(full) != 3 || !strings.Contains(ansi.Strip(full[0]), "one") || !strings.Contains(ansi.Strip(full[0]), "four") {
		t.Errorf("expanded = %q, want the columns side by side", full)
	}
	for _, l := range helpLines(h, footKeys{}, 6, 3) {
		if ansi.StringWidth(l) > 6 {
			t.Errorf("line %q is wider than 6", l)
		}
	}
}
