package main

// Home directory abbreviation. This file is the same in every tool of the
// family that shows paths.

import (
	"os"
	"strings"
)

// tildePath abbreviates the home prefix of p to ~. Only whole path elements
// count: /Users/ann2 is not inside /Users/ann.
func tildePath(p, home string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// homeDir is the current user's home, "" when it cannot be told.
func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// homeRel is tildePath against the current user's home.
func homeRel(p string) string { return tildePath(p, homeDir()) }
