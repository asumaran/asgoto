package main

// The filter input, the same in every tool of the family. Inside herdr's
// popup the prompt is just the arrow, because the pane's title already names
// the tool, and a placeholder says what the filter searches. Run on its own
// in a terminal, the prompt carries the tool's name. A build that is not a
// release says "(dev)" on the edge over the input, after the status, where
// the frame already keeps that kind of information.
//
// This file is the same in every tool of the family.

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stDev    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
)

// inPopup reports whether herdr launched this process as a plugin pane.
func inPopup() bool { return os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "" }

// devBuild reports whether this binary was built without a release version
// (see `version` in main.go).
func devBuild() bool { return !strings.HasPrefix(version, "v") }

// promptText is the input's prompt: the arrow, after the tool's name when it
// runs outside herdr's popup.
func promptText(name string) string {
	if inPopup() {
		return stPrompt.Render("❯ ")
	}
	return stPrompt.Render(name + " ❯ ")
}

// newFilterInput builds the focused filter input. The prompt string already
// carries its colors, so the prompt style is left empty.
func newFilterInput(name, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = promptText(name)
	ti.Placeholder = placeholder
	st := ti.Styles()
	st.Focused.Prompt, st.Blurred.Prompt = lipgloss.NewStyle(), lipgloss.NewStyle()
	st.Focused.Placeholder, st.Blurred.Placeholder = stDim, stDim
	ti.SetStyles(st)
	ti.Focus()
	return ti
}

// sizeInput fits the input into width cells of the input line, the prompt
// included. The input needs a width of its own: without one bubbles shows only
// the first rune of the placeholder.
func sizeInput(ti *textinput.Model, width int) {
	ti.SetWidth(max(1, width-ansi.StringWidth(ti.Prompt)-1)) // 1: the cursor's cell
}

// devMark is what a build that is not a release shows.
func devMark() string {
	if !devBuild() {
		return ""
	}
	return stPrompt.Render("(") + stDev.Render("dev") + stPrompt.Render(")")
}

// withDevMark adds devMark after the status on the edge over the input.
func withDevMark(status string) string {
	mark := devMark()
	switch {
	case mark == "":
		return status
	case status == "":
		return mark
	}
	return status + " " + mark
}
