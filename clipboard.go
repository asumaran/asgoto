package main

// Copying the selection to the system clipboard.
//
// This file is the same in every tool of the family.

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// copyCmd copies text and reports how it went with a flashMsg. label is what
// the confirmation calls it when the text itself is too long to show.
func copyCmd(tool, label, text string) tea.Cmd {
	if text == "" {
		return func() tea.Msg { return flashMsg("nothing to copy") }
	}
	return func() tea.Msg {
		if err := copyToClipboard(tool, text); err != nil {
			return flashMsg("copy failed: " + err.Error())
		}
		if label == "" {
			label = text
		}
		return flashMsg("copied " + label)
	}
}

// copyToClipboard feeds s to the clipboard command on its stdin.
func copyToClipboard(tool, s string) error {
	argv := clipboardCmd(tool)
	if len(argv) == 0 {
		return errors.New("no clipboard command found (wl-copy, xclip or xsel)")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// clipboardCmd is <TOOL>_CLIPBOARD when set (the pty checks point it at a
// logging stub), pbcopy on macOS, else the first of wl-copy, xclip and xsel
// that is installed.
func clipboardCmd(tool string) []string {
	if bin := os.Getenv(strings.ToUpper(tool) + "_CLIPBOARD"); bin != "" {
		return []string{bin}
	}
	if runtime.GOOS == "darwin" {
		return []string{"pbcopy"}
	}
	for _, argv := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv
		}
	}
	return nil
}
