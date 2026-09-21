package main

// A word that takes the help line for a moment: a confirmation ("copied
// abc1234") in green, or a key that had nothing to do ("nothing to copy") in
// the error color. Commands report one with a flashMsg or a flashErrMsg; the
// model keeps a flash, sets it, and clears it when the timer of that same
// message fires.
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

// flashMsg asks the model to flash its text (flash.set) and flashErrMsg to
// flash it as a failure (flash.fail); clearFlashMsg removes the flash with
// that sequence number, unless a newer one replaced it.
type flashMsg string
type flashErrMsg string
type clearFlashMsg int

type flash struct {
	text string
	bad  bool // a failure, not a confirmation
	seq  int
}

// set shows s as a confirmation and returns the timer that takes it away.
func (f *flash) set(s string) tea.Cmd { return f.show(s, false) }

// fail shows s as a failure: the key was taken and could do nothing.
func (f *flash) fail(s string) tea.Cmd { return f.show(s, true) }

func (f *flash) show(s string, bad bool) tea.Cmd {
	f.text, f.bad = s, bad
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
	st := stFlash
	if f.bad {
		st = stError
	}
	return truncate(st.Render(f.text), max(0, width))
}
