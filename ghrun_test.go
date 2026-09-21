package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGhRunSaysWhatWentWrong(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if _, err := ghRun(context.Background(), "api"); err == nil || err.Error() != "gh not found (install the GitHub CLI)" {
		t.Errorf("without gh: %v", err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = ok ]; then echo fine; exit 0; fi\necho 'not logged in' >&2\necho 'more' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	if out, err := ghRun(context.Background(), "ok"); err != nil || string(out) != "fine\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
	if _, err := ghRun(context.Background(), "api"); err == nil || err.Error() != "gh: not logged in" {
		t.Errorf("a failure is the first line of stderr: %v", err)
	}
}
