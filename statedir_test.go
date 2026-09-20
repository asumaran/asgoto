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
	// on its own: the directory herdr gives the plugin
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got := stateDirFor("astool"); got != filepath.Join("/xdg", "herdr", "plugins", "asumaran.astool") {
		t.Errorf("standalone = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got := stateDirFor("astool"); got != filepath.Join("/home/me", ".local", "state", "herdr", "plugins", "asumaran.astool") {
		t.Errorf("without XDG_STATE_HOME = %q", got)
	}
}
