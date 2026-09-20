package main

import "testing"

func TestTildePath(t *testing.T) {
	const home = "/Users/ann"
	for in, want := range map[string]string{
		"/Users/ann":          "~",
		"/Users/ann/wt/shop":  "~/wt/shop",
		"/Users/ann2/wt/shop": "/Users/ann2/wt/shop",
		"/tmp/x":              "/tmp/x",
		"":                    "",
	} {
		if got := tildePath(in, home); got != want {
			t.Errorf("tildePath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := tildePath("/Users/ann/x", ""); got != "/Users/ann/x" {
		t.Errorf("tildePath without a home = %q", got)
	}
}

func TestHomeRel(t *testing.T) {
	t.Setenv("HOME", "/Users/ann")
	if got := homeRel("/Users/ann/Developer"); got != "~/Developer" {
		t.Errorf("homeRel = %q", got)
	}
	if got := homeRel("/Users/annex"); got != "/Users/annex" {
		t.Errorf("homeRel on a sibling of home = %q", got)
	}
}
