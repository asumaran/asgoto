package main

// A JSON file in the state dir: the caches that make the popup open at once.
// A cache is only ever a hint, so nothing here reports an error: a file that
// is missing or does not parse reads as nothing, and a write that fails is
// tried again by the next run.
//
// This file is the same in every tool of the family that keeps one.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// readJSONFile reads the file at path into v, which keeps its value when
// there is no file or it does not parse.
func readJSONFile(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}

// writeJSONFile writes v to the file at path.
func writeJSONFile(path string, v any) {
	if data, err := json.Marshal(v); err == nil {
		writeFileAtomic(path, data)
	}
}

// writeFileAtomic puts data at path through a temporary file and a rename, so
// a popup closed mid-write, or two of them writing at once, never leave half
// a file for the next run to read.
func writeFileAtomic(path string, data []byte) {
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
