package main

import "testing"

func TestHerdrBin(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	if got := herdrBin(); got != "herdr" {
		t.Errorf("herdrBin without HERDR_BIN_PATH = %q", got)
	}
	t.Setenv("HERDR_BIN_PATH", "/opt/herdr/bin/herdr")
	if got := herdrBin(); got != "/opt/herdr/bin/herdr" {
		t.Errorf("herdrBin = %q", got)
	}
}
