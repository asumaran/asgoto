package main

// A setting the tool remembers between runs: one plain-text file each, in
// the tool's state directory (statedir.go). Every option of the panel
// (panel.go) is kept this way, per tool.
//
// This file is the same in every tool of the family that needs it.

import (
	"os"
	"path/filepath"
	"strings"
)

// loadSetting reads a setting, "" when it was never saved.
func loadSetting(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveSetting is best effort: a read-only state dir only costs the
// persistence.
func saveSetting(dir, name, value string) {
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644)
}
