package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cache.json")
	got := map[string]int{"kept": 1}
	readJSONFile(path, &got)
	if got["kept"] != 1 {
		t.Errorf("a missing file must leave the value alone: %v", got)
	}
	writeJSONFile(path, map[string]int{"a": 2})
	got = nil
	readJSONFile(path, &got)
	if got["a"] != 2 {
		t.Errorf("round trip: %v", got)
	}
	// a rewrite replaces the file and leaves no temporary file behind
	writeJSONFile(path, map[string]int{"b": 3})
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("want the cache alone in its dir, got %d entries", len(entries))
	}
	if err := os.WriteFile(path, []byte("{half"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = map[string]int{"kept": 1}
	readJSONFile(path, &got)
	if got["kept"] != 1 {
		t.Errorf("a file that does not parse reads as nothing: %v", got)
	}
}
