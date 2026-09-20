package main

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncateAndPad(t *testing.T) {
	if got := truncate("structured data", 8); got != "structu…" {
		t.Errorf("truncate = %q", got)
	}
	if truncate("abc", 3) != "abc" || truncate("abc", 0) != "" || truncate("abc", -2) != "" {
		t.Errorf("a text that fits is untouched; no room, no text")
	}
	styled := "\x1b[1mstructured\x1b[m data"
	if got := truncate(styled, 8); ansi.StringWidth(got) != 8 || ansi.Strip(got) != "structu…" {
		t.Errorf("truncate counts cells, not escape codes: %q", got)
	}
	if got := padRight("añó", 5); got != "añó  " || ansi.StringWidth(padRight("structured", 4)) != 4 {
		t.Errorf("padRight = %q", got)
	}
	if got := padLeft("2h", 4); got != "  2h" {
		t.Errorf("padLeft = %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"one":                     "one",
		"\n fatal: bad ref\nhint": "fatal: bad ref",
		"title\n\nbody":           "title",
	} {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "file") + ", " + plural(0, "file") + ", " + plural(12, "commit"); got != "1 file, 0 files, 12 commits" {
		t.Errorf("plural = %q", got)
	}
}
