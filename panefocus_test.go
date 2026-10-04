package main

import "testing"

func TestPaneFocusRequest(t *testing.T) {
	got, err := paneFocusRequest("w45:pF")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"pane.focus","method":"pane.focus","params":{"pane_id":"w45:pF"}}` + "\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
