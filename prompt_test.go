package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPromptNamesTheToolOnlyOutsideThePopup(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_ENTRYPOINT_ID", "")
	if got := ansi.Strip(promptText("astool")); got != "astool ❯ " {
		t.Errorf("standalone prompt = %q", got)
	}
	t.Setenv("HERDR_PLUGIN_ENTRYPOINT_ID", "picker")
	if got := ansi.Strip(promptText("astool")); got != "❯ " {
		t.Errorf("popup prompt = %q, want the arrow alone: the pane's title names the tool", got)
	}
	ti := newFilterInput("astool", "Search by path…")
	sizeInput(&ti, 40)
	if view := ansi.Strip(ti.View()); !strings.HasPrefix(view, "❯ ") || !strings.Contains(view, "earch by path…") {
		t.Errorf("empty input = %q, want the prompt and the placeholder", view)
	}
	ti.SetValue("cart")
	if view := ansi.Strip(ti.View()); !strings.Contains(view, "cart") || strings.Contains(view, "by path") {
		t.Errorf("typed input = %q, want the text instead of the placeholder", view)
	}
}

func TestDevMarkFollowsTheStatus(t *testing.T) {
	release := version
	t.Cleanup(func() { version = release })

	version = "dev"
	if got := ansi.Strip(withDevMark("[in ~/dir]")); got != "[in ~/dir] (dev)" {
		t.Errorf("status = %q, want the mark after it", got)
	}
	if got := ansi.Strip(withDevMark("")); got != "(dev)" {
		t.Errorf("without a status the mark stands alone: %q", got)
	}
	version = "v1.2.3"
	if got := withDevMark("[in ~/dir]"); got != "[in ~/dir]" || devMark() != "" {
		t.Errorf("a release shows no mark: %q", got)
	}
	ti := newFilterInput("astool", "Search by path…")
	sizeInput(&ti, 40)
	if w := ansi.StringWidth(ti.View()); w > 40 {
		t.Errorf("the sized input is %d cells wide, want at most 40", w)
	}
}
