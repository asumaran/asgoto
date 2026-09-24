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

// noPanelKeys is a help line without the panel's key: the foot with context
// falls back to the plain help.
type noPanelKeys struct{}

func (noPanelKeys) ShortHelp() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))}
}
func (noPanelKeys) FullHelp() [][]key.Binding { return [][]key.Binding{noPanelKeys{}.ShortHelp()} }

func TestFootWithContext(t *testing.T) {
	h := help.New()
	h.SetWidth(8) // the model's width never cuts the panel key
	info := stInfo.Render("~/wt/shop/fix  main")
	plain := func(s string) string { return ansi.Strip(s) }

	line := footLine(flash{}, "", info, h, footKeys{}, 60)
	if w := ansi.StringWidth(line); w != 60 || !strings.HasPrefix(plain(line), "~/wt/shop/fix  main") || !strings.HasSuffix(plain(line), "f1 options") {
		t.Errorf("foot = %q (%d cells), want the context, the panel key at the right end, 60 cells", plain(line), w)
	}
	if got := footRoom(h, footKeys{}, 60); got != 60-len("f1 options")-2 {
		t.Errorf("footRoom = %d, want the width minus the panel key and the gap", got)
	}

	f := flash{text: "copied it"}
	line = footLine(f, "", info, h, footKeys{}, 60)
	if p := plain(line); !strings.HasPrefix(p, "copied it") || strings.Contains(p, "main") || !strings.HasSuffix(p, "f1 options") {
		t.Errorf("a flash takes the context's place, not the panel key's: %q", p)
	}
	line = footLine(flash{}, "boom", info, h, footKeys{}, 60)
	if p := plain(line); !strings.HasPrefix(p, "boom") || strings.Contains(p, "main") || !strings.HasSuffix(p, "f1 options") {
		t.Errorf("a notice takes the context's place, not the panel key's: %q", p)
	}

	line = footLine(flash{}, "", info, h, footKeys{}, 20)
	if p := plain(line); ansi.StringWidth(line) != 20 || !strings.HasSuffix(p, "f1 options") || !strings.HasPrefix(p, "~/wt/sh…") {
		t.Errorf("a narrow foot cuts the context, never the panel key: %q", p)
	}
	if line := footLine(flash{}, "", info, h, footKeys{}, 6); ansi.StringWidth(line) > 6 {
		t.Errorf("narrower than the panel key, the line is still cut to the width: %q", plain(line))
	}
	if got := footRoom(h, footKeys{}, 4); got != 1 {
		t.Errorf("footRoom below the panel key = %d, want 1", got)
	}

	h.SetWidth(60) // the help, unlike the panel key, follows the model's width
	if line := footLine(flash{}, "", "", h, footKeys{}, 60); !strings.HasPrefix(plain(line), "enter open") {
		t.Errorf("without context the foot is the help: %q", plain(line))
	}
	if line := footLine(flash{}, "", info, h, noPanelKeys{}, 60); !strings.HasPrefix(plain(line), "enter open") {
		t.Errorf("without a panel key the foot is the help: %q", plain(line))
	}
	if hint := panelHint(h, footKeys{}); ansi.Strip(hint) != "f1 options" {
		t.Errorf("panelHint = %q, want the panel key alone", ansi.Strip(hint))
	}
}
