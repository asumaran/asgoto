package main

// Where a tool keeps its runtime state. This file is the same in every tool
// of the family.

import (
	"os"
	"path/filepath"
)

// stateDirFor is the state directory of the tool called name. herdr creates
// one per plugin and injects it: runtime state must live there, not in the
// plugin's checkout. Run on its own, the tool falls back to a fixed path under
// the config home.
func stateDirFor(name string) string {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".config")
		}
	}
	return filepath.Join(base, "herdr", name+"-tui")
}
