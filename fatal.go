package main

// An error that keeps the tool from starting.
//
// This file is the same in every tool of the family that needs it.

import (
	"fmt"
	"os"
)

// fatal reports why there is nothing to show and exits 1. herdr's popup
// closes the instant the process exits and takes stderr with it, so there the
// message is held until enter; in a shell it is a plain error.
func fatal(tool, msg string) {
	fmt.Fprintln(os.Stderr, tool+":", msg)
	if inPopup() {
		fmt.Fprint(os.Stderr, "press enter to close…")
		_, _ = fmt.Scanln()
	}
	os.Exit(1)
}
