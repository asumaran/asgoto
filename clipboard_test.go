package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyCmdFeedsTheToolsClipboard(t *testing.T) {
	log := filepath.Join(t.TempDir(), "clip")
	stub := filepath.Join(t.TempDir(), "clipboard")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTOOL_CLIPBOARD", stub)
	if msg := copyCmd("astool", "", "abc1234")(); msg != flashMsg("copied abc1234") {
		t.Errorf("msg = %q", msg)
	}
	if got, _ := os.ReadFile(log); string(got) != "abc1234" {
		t.Errorf("the clipboard got %q", got)
	}
	if msg := copyCmd("astool", "the path", "/a/very/long/path")(); msg != flashMsg("copied the path") {
		t.Errorf("a label replaces the text in the confirmation: %q", msg)
	}
	if msg := copyCmd("astool", "", "")(); msg != flashErrMsg("nothing to copy") {
		t.Errorf("empty text: %q", msg)
	}
	t.Setenv("ASTOOL_CLIPBOARD", filepath.Join(t.TempDir(), "missing"))
	if msg, ok := copyCmd("astool", "", "x")().(flashErrMsg); !ok || len(msg) < 12 || msg[:12] != "copy failed:" {
		t.Errorf("a failing command is reported as a failure: %q", msg)
	}
}
