package main

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFlashIsClearedByItsOwnTimerOnly(t *testing.T) {
	var f flash
	if f.view(40) != "" {
		t.Errorf("no flash yet: %q", f.view(40))
	}
	if f.set("copied abc") == nil {
		t.Fatal("set must return the timer")
	}
	first := clearFlashMsg(f.seq)
	f.set("copied def")
	f.clear(first)
	if got := ansi.Strip(f.view(40)); got != "copied def" {
		t.Errorf("an older timer must not clear a newer flash: %q", got)
	}
	if got := ansi.Strip(f.view(6)); got != "copie…" {
		t.Errorf("the flash is cut to the width: %q", got)
	}
	f.clear(clearFlashMsg(f.seq))
	if f.view(40) != "" {
		t.Errorf("its own timer clears it: %q", f.view(40))
	}
}

// A failure shows in the error color, and a confirmation after it is a
// confirmation again.
func TestFlashFailure(t *testing.T) {
	var f flash
	if f.fail("nothing to copy") == nil || !f.bad || f.text != "nothing to copy" {
		t.Fatalf("fail: %+v", f)
	}
	if got := f.view(40); got != truncate(stError.Render("nothing to copy"), 40) {
		t.Errorf("a failure is drawn as an error: %q", got)
	}
	f.set("copied abc")
	if f.bad || f.view(40) != truncate(stFlash.Render("copied abc"), 40) {
		t.Errorf("a confirmation after a failure: %+v", f)
	}
}
