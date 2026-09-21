package main

import (
	"path/filepath"
	"testing"
)

func TestSettingRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "astool") // made on the first save
	if got := loadSetting(dir, "scope"); got != "" {
		t.Errorf("a setting never saved = %q, want empty", got)
	}
	saveSetting(dir, "scope", "all")
	saveSetting(dir, "layout", "rows")
	if got := loadSetting(dir, "scope"); got != "all" {
		t.Errorf("scope = %q, want all", got)
	}
	saveSetting(dir, "scope", "here")
	if loadSetting(dir, "scope") != "here" || loadSetting(dir, "layout") != "rows" {
		t.Errorf("each setting has its own file: %q %q", loadSetting(dir, "scope"), loadSetting(dir, "layout"))
	}
	saveSetting("", "scope", "all") // no state dir: nothing to do, nothing to break
}
