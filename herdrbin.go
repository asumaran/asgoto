package main

// Where the herdr executable is. This file is the same in every tool of the
// family that talks to herdr.

import "os"

// herdrBin resolves the herdr executable. Plugin commands receive
// HERDR_BIN_PATH from the running server; fall back to PATH lookup so the
// binary also works from a plain pane.
func herdrBin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}
