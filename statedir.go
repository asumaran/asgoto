package main

// Where a tool keeps its runtime state. This file is the same in every tool
// of the family.

import (
	"os"
	"path/filepath"
)

// pluginOwner prefixes the tool's name in its herdr plugin id.
const pluginOwner = "asumaran."

// stateDirFor is the state directory of the tool called name. herdr creates
// one per plugin and injects it: runtime state must live there, not in the
// plugin's checkout. Run on its own, the tool works out the directory herdr
// would have given it, so the popup and a run from the shell share their
// settings and caches.
func stateDirFor(name string) string {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".local", "state")
		}
	}
	return filepath.Join(base, "herdr", "plugins", pluginOwner+name)
}
