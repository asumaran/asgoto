package main

// A confirmation that takes the help line for a moment ("copied abc1234").
// Commands report one with a flashMsg; the model keeps a flash, sets it, and
// clears it when the timer of that same message fires.
//
// This file is the same in every tool of the family.

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var stFlash = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

// flashFor is how long a confirmation stays on the help line.
const flashFor = 2 * time.Second

// flashMsg asks the model to flash its text; clearFlashMsg removes the flash
// with that sequence number, unless a newer one replaced it.
type flashMsg string
type clearFlashMsg int

type flash struct {
	text string
	seq  int
}

// set shows s and returns the timer that takes it away.
func (f *flash) set(s string) tea.Cmd {
	f.text = s
	f.seq++
	seq := f.seq
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return clearFlashMsg(seq) })
}

func (f *flash) clear(msg clearFlashMsg) {
	if int(msg) == f.seq {
		f.text = ""
	}
}

// view is the flash fitted to width cells, "" while there is none.
func (f flash) view(width int) string {
	if f.text == "" {
		return ""
	}
	return truncate(stFlash.Render(f.text), max(0, width))
}
