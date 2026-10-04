package main

// How the family shows a herdr agent status: the 1-rune dot, its colors and
// the order statuses are ranked in. It mirrors herdr's own sidebar, including
// the two settings that change it (`ui.status_indicators` and the dracula
// theme), read from herdr's config.toml by applyHerdrConfig. Only
// presentation and order: what a parent row aggregates is each tool's call.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// Status dots, mirroring herdr's sidebar state_dot.
var (
	stDotBlocked = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	stDotWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	stDotDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))  // teal
	stDotIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	stDotNone    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // dim
)

// statusRank orders agent statuses, mirroring herdr's
// workspace_attention_priority: blocked beats done, done beats working,
// working beats idle, idle beats none/unknown.
func statusRank(s string) int {
	switch s {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	default: // "unknown", "" (shell / no agent)
		return 0
	}
}

// statusDot renders the 1-rune indicator for an agent status, mirroring
// herdr's status_icon in both of its styles (see statusSymbols): "dots"
// (blocked/working/done filled, idle hollow, none/unknown dim) or "symbols"
// (× ◐ ✓ for the first three).
func statusDot(s string) string {
	glyph := func(dot, symbol string) string {
		if statusSymbols {
			return symbol
		}
		return dot
	}
	switch s {
	case "blocked":
		return stDotBlocked.Render(glyph("●", "×"))
	case "working":
		return stDotWorking.Render(glyph("●", "◐"))
	case "done":
		return stDotDone.Render(glyph("●", "✓"))
	case "idle":
		return stDotIdle.Render("○")
	default: // "unknown", "" (shell / no agent)
		return stDotNone.Render("·")
	}
}

// statusSymbols mirrors herdr's `ui.status_indicators = "symbols"`; false is
// its default, "dots". Set once in main by applyHerdrConfig.
var statusSymbols bool

// herdrConfigPath resolves herdr's config.toml the way herdr does
// (config_path in src/config/io.rs): HERDR_CONFIG_PATH, then
// $XDG_CONFIG_HOME/herdr, then ~/.config/herdr.
func herdrConfigPath() string {
	if p := os.Getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "herdr", "config.toml")
}

var (
	reTomlTable  = regexp.MustCompile(`^\s*\[\s*([^\]]+?)\s*\]\s*(#.*)?$`)
	reTomlString = regexp.MustCompile(`^\s*([A-Za-z0-9_.\s-]+?)\s*=\s*["']([^"']*)["']`)
)

// herdrConfigString reads one string value (`table.key`) out of herdr's
// config.toml. A line scan instead of a TOML dependency: the keys the family
// needs are plain strings, written either under their [table] or as a dotted
// top-level key. "" when absent.
func herdrConfigString(config, table, key string) string {
	current := ""
	for _, line := range strings.Split(config, "\n") {
		if m := reTomlTable.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := reTomlString.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.Join(strings.Fields(m[1]), "")
		if (current == table && name == key) || (current == "" && name == table+"."+key) {
			return m[2]
		}
	}
	return ""
}

// applyHerdrConfig mirrors the two herdr settings the status dot depends on:
// `ui.status_indicators` (glyph style; anything but "symbols" is herdr's
// default, "dots") and `theme.name`. Only dracula's palette is mirrored
// (Palette::dracula in herdr's src/app/state.rs: red, yellow, teal, green,
// overlay0); every other theme keeps the terminal's ANSI colors.
func applyHerdrConfig(config string) {
	statusSymbols = herdrConfigString(config, "ui", "status_indicators") == "symbols"
	if strings.ToLower(herdrConfigString(config, "theme", "name")) == "dracula" {
		stDotBlocked = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
		stDotWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c"))
		stDotDone = lipgloss.NewStyle().Foreground(lipgloss.Color("#8be9fd"))
		stDotIdle = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b"))
		stDotNone = lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4"))
	}
}

// loadHerdrConfig applies herdr's config.toml, if there is one.
func loadHerdrConfig() {
	if data, err := os.ReadFile(herdrConfigPath()); err == nil {
		applyHerdrConfig(string(data))
	}
}
