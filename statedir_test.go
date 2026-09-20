package main

import (
	"path/filepath"
	"testing"
)

func TestStateDirFor(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/plugins/state")
	if got := stateDirFor("astool"); got != "/plugins/state" {
		t.Errorf("inside herdr = %q, want the injected dir", got)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := stateDirFor("astool"); got != filepath.Join("/xdg", "herdr", "astool-tui") {
		t.Errorf("standalone = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got := stateDirFor("astool"); got != filepath.Join("/home/me", ".config", "herdr", "astool-tui") {
		t.Errorf("without XDG_CONFIG_HOME = %q", got)
	}
}
