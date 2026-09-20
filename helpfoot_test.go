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
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")), helpBinding(true)}
}

func (footKeys) FullHelp() [][]key.Binding {
	b := func(k, d string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, d)) }
	return [][]key.Binding{{b("a", "one"), b("b", "two"), b("c", "three")}, {b("d", "four"), helpBinding(true)}}
}

func TestHelpKeyIsNeverText(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyF1}} {
		if !isHelpKey(k) {
			t.Errorf("%s should open the panel", k.String())
		}
	}
	if isHelpKey(tea.KeyPressMsg{Code: '?', Text: "?"}) {
		t.Errorf("? is text for the filter, not a help key")
	}
}

func TestHelpLine(t *testing.T) {
	h := help.New()
	h.SetWidth(80)
	h.ShowAll = true // whatever the model says, the foot is one line
	line := ansi.Strip(helpLine(h, footKeys{}, 80))
	if strings.Contains(line, "\n") || !strings.Contains(line, "f1 options") || strings.Contains(line, "three") {
		t.Errorf("foot = %q, want the short help on one line", line)
	}
	if got := helpBinding(false).Help().Desc; got != "help" {
		t.Errorf("a tool without options offers %q, want help", got)
	}
	if w := ansi.StringWidth(helpLine(h, footKeys{}, 6)); w > 6 {
		t.Errorf("the line is %d cells wide, want 6 at most", w)
	}
}
