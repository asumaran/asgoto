package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestHerdrConfigString covers reading herdr's config.toml without a TOML
// parser: only the key under its own table (or dotted at the top level)
// counts.
func TestHerdrConfigString(t *testing.T) {
	cases := []struct{ name, config, want string }{
		{"empty", "", ""},
		{"under ui", "onboarding = false\n[ui]\npane_gaps = true\nstatus_indicators = \"symbols\"\n[theme]\nname = \"dracula\"\n", "symbols"},
		{"dotted top-level", "ui.status_indicators = 'symbols'\n[theme]\n", "symbols"},
		{"other table", "[theme]\nstatus_indicators = \"symbols\"\n", ""},
		{"subtable of ui", "[ui]\n[ui.toast]\nstatus_indicators = \"symbols\"\n", ""},
		{"commented out", "[ui]\n# status_indicators = \"symbols\"\n", ""},
		{"inline table key", "[ui]\ntab_bar_right = [\n  { status_indicators = \"symbols\" },\n]\n", ""},
	}
	for _, c := range cases {
		if got := herdrConfigString(c.config, "ui", "status_indicators"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestApplyHerdrConfig covers the two settings the gutter mirrors: the glyph
// style, and dracula's palette (any other theme keeps the ANSI colors).
func TestApplyHerdrConfig(t *testing.T) {
	prevSymbols := statusSymbols
	prev := []lipgloss.Style{stDotBlocked, stDotWorking, stDotDone, stDotIdle, stDotNone}
	defer func() {
		statusSymbols = prevSymbols
		stDotBlocked, stDotWorking, stDotDone, stDotIdle, stDotNone = prev[0], prev[1], prev[2], prev[3], prev[4]
	}()
	ansi := stDotBlocked.GetForeground()

	applyHerdrConfig("[ui]\nstatus_indicators = \"emoji\"\n[theme]\nname = \"nord\"\n")
	if statusSymbols || stDotBlocked.GetForeground() != ansi {
		t.Errorf("unknown style / other theme: symbols=%v fg=%v, want dots and ANSI", statusSymbols, stDotBlocked.GetForeground())
	}
	applyHerdrConfig("[ui]\nstatus_indicators = \"symbols\"\n[theme]\nname = \"Dracula\"\n")
	if want := lipgloss.Color("#ff5555"); !statusSymbols || stDotBlocked.GetForeground() != want {
		t.Errorf("symbols + dracula: symbols=%v fg=%v, want true and %v", statusSymbols, stDotBlocked.GetForeground(), want)
	}
}

func TestStatusDotStyles(t *testing.T) {
	defer func(prev bool) { statusSymbols = prev }(statusSymbols)
	for _, c := range []struct {
		symbols bool
		want    string // blocked working done idle none
	}{
		{false, "● ● ● ○ ·"},
		{true, "× ◐ ✓ ○ ·"},
	} {
		statusSymbols = c.symbols
		var got []string
		for _, s := range []string{"blocked", "working", "done", "idle", ""} {
			got = append(got, ansi.Strip(statusDot(s)))
		}
		if g := strings.Join(got, " "); g != c.want {
			t.Errorf("symbols=%v: got %q, want %q", c.symbols, g, c.want)
		}
	}
}

// TestStatusRank covers herdr's attention order: blocked > done > working >
// idle > anything else.
func TestStatusRank(t *testing.T) {
	order := []string{"blocked", "done", "working", "idle", "unknown"}
	for i := 1; i < len(order); i++ {
		if statusRank(order[i-1]) <= statusRank(order[i]) {
			t.Errorf("%s should rank above %s", order[i-1], order[i])
		}
	}
	if statusRank("") != statusRank("unknown") {
		t.Errorf("no agent and unknown should rank alike")
	}
}
