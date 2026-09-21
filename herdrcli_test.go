package main

import (
	"os"
	"path/filepath"
	"testing"
)

// herdrScript points HERDR_BIN_PATH at a script.
func herdrScript(t *testing.T, script string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", bin)
}

func TestHerdrRunReturnsStdout(t *testing.T) {
	herdrScript(t, `echo "$1 $2"`)
	out, err := herdrRun("pane", "list")
	if err != nil || string(out) != "pane list\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
	if err := herdrDo("workspace", "focus"); err != nil {
		t.Errorf("herdrDo: %v", err)
	}
}

// A failure is said the way herdr said it, on whichever stream.
func TestHerdrRunReportsHerdrsMessage(t *testing.T) {
	for _, script := range []string{
		`echo '{"error":{"message":"no such pane"}}' >&2; exit 1`,
		`echo '{"error":{"message":"no such pane"}}'; exit 1`,
	} {
		herdrScript(t, script)
		if _, err := herdrRun("pane", "focus"); err == nil || err.Error() != "no such pane" {
			t.Errorf("%s: err = %v", script, err)
		}
	}
	herdrScript(t, `exit 3`)
	if err := herdrDo("x"); err == nil || err.Error() != "exit status 3" {
		t.Errorf("without a message the exit status is what is left: %v", err)
	}
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "missing"))
	if _, err := herdrRun("x"); err == nil {
		t.Errorf("a herdr that cannot run must be an error")
	}
}
