package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestListMouseGeometry(t *testing.T) {
	// a list 20 cells wide and 5 rows tall that starts on screen row 3
	for _, tc := range []struct {
		x, y int
		want bool
	}{{1, 3, true}, {20, 7, true}, {0, 3, false}, {21, 3, false}, {5, 2, false}, {5, 8, false}} {
		if got := inList(tc.x, tc.y, 3, 20, 5); got != tc.want {
			t.Errorf("inList(%d, %d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	if i, ok := rowUnder(5, 3, 10, 40); !ok || i != 12 {
		t.Errorf("rowUnder = %d, %v, want row 12 of a list scrolled by 10", i, ok)
	}
	if _, ok := rowUnder(7, 3, 0, 3); ok {
		t.Errorf("a click under the last row is not a row")
	}
	if _, ok := rowUnder(2, 3, 0, 3); ok {
		t.Errorf("a click over the first row is not a row")
	}
}

func TestWheelKey(t *testing.T) {
	if k, ok := wheelKey(tea.MouseWheelMsg{Button: tea.MouseWheelUp}); !ok || k.Code != tea.KeyUp {
		t.Errorf("wheel up = %v, %v", k, ok)
	}
	if k, ok := wheelKey(tea.MouseWheelMsg{Button: tea.MouseWheelDown}); !ok || k.Code != tea.KeyDown {
		t.Errorf("wheel down = %v, %v", k, ok)
	}
	if _, ok := wheelKey(tea.MouseWheelMsg{Button: tea.MouseWheelLeft}); ok {
		t.Errorf("a sideways wheel is not a list key")
	}
}
