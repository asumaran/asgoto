package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// goldenPRs is the format's one example: what asmeta writes and every reader
// reads.
const goldenPRs = `{
 "version": 1,
 "last_query_started_at": "2026-10-03T15:20:00Z",
 "backoff_until": null,
 "branches": {
  "asumaran/asgoto": {
   "feat/shared-pr-cache": {"url": "https://github.com/asumaran/asgoto/pull/41", "checked_at": "2026-10-03T15:20:01Z"},
   "fix/old-thing": {"url": null, "checked_at": "2026-10-03T15:20:01Z"}
  },
  "me/bubbletea": {
   "fix-x": {"url": "https://github.com/charm/bubbletea/pull/123", "checked_at": "2026-10-03T15:20:01Z"}
  }
 },
 "pulls": {
  "https://github.com/asumaran/asgoto/pull/41": {"repo": "asumaran/asgoto", "number": 41, "head": "feat/shared-pr-cache", "base": "main", "state": "draft", "title": "feat(prs): read the shared PR cache", "body": "first 1500 chars", "updated_at": "2026-10-03T15:10:00Z", "checked_at": "2026-10-03T15:20:01Z"},
  "https://github.com/charm/bubbletea/pull/123": {"repo": "charm/bubbletea", "number": 123, "head": "fix-x", "base": "main", "state": "open", "title": "fix: x", "updated_at": "2026-10-02T10:00:00Z", "checked_at": "2026-10-03T15:20:01Z"}
 }
}`

func TestSharedPRsLookup(t *testing.T) {
	s := decodeSharedPRs([]byte(goldenPRs))
	pr, checked, at := s.branch("Asumaran/ASGOTO", "feat/shared-pr-cache")
	if !checked || pr == nil || pr.Number != 41 || pr.State != "draft" || at.IsZero() {
		t.Errorf("a PR: %+v %v %v", pr, checked, at)
	}
	if pr, checked, _ := s.branch("asumaran/asgoto", "fix/old-thing"); !checked || pr != nil {
		t.Errorf("checked, no PR: %+v %v", pr, checked)
	}
	if _, checked, _ := s.branch("asumaran/asgoto", "never"); checked {
		t.Error("a branch never checked must say so")
	}
	if pr, _, _ := s.branch("me/bubbletea", "fix-x"); pr == nil || pr.Repo != "charm/bubbletea" {
		t.Errorf("a fork's branch maps to the upstream PR: %+v", pr)
	}
	p, ok := s.pull("https://github.com/asumaran/asgoto/pull/41")
	if !ok || !p.newerThan(time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)) || p.newerThan(p.UpdatedAt) {
		t.Errorf("pull by URL: %+v %v", p, ok)
	}
}

func TestSharedPRsBadFiles(t *testing.T) {
	for _, doc := range []string{"", "{", `{"version": 2, "pulls": {}}`, `{"version": 1, "pulls": {"x": {"number": "nan"}}}`} {
		s := decodeSharedPRs([]byte(doc))
		if s.Version != sharedPRVersion || len(s.Pulls) != 0 || s.Branches == nil {
			t.Errorf("%q must read as empty: %+v", doc, s)
		}
	}
}

func TestSharedPRState(t *testing.T) {
	cases := []struct {
		state string
		draft bool
		want  string
	}{{"OPEN", false, "open"}, {"OPEN", true, "draft"}, {"CLOSED", true, "closed"}, {"MERGED", false, "merged"}}
	for _, c := range cases {
		if got := sharedPRState(c.state, c.draft); got != c.want {
			t.Errorf("%s draft=%v = %s, want %s", c.state, c.draft, got, c.want)
		}
	}
	for b, want := range map[string]bool{"": true, "main": true, "master": true, "develop": true, "feat/x": false} {
		if prSkipBranch(b) != want {
			t.Errorf("prSkipBranch(%q) = %v", b, !want)
		}
	}
}

func TestSharedPRsMergeAndSave(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(t.TempDir(), "plugins", "asumaran.astool"))
	now := time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)
	s := decodeSharedPRs([]byte(goldenPRs))
	// a branch nobody checked for a month goes, and so does its PR
	s.Branches["old/repo"] = map[string]sharedBranch{"b": {URL: sharedPRPtr("https://github.com/old/repo/pull/1"), CheckedAt: now.Add(-31 * 24 * time.Hour)}}
	s.Pulls["https://github.com/old/repo/pull/1"] = sharedPR{Repo: "old/repo", Number: 1}
	s.merge(map[string]sharedPRCheck{
		"ASUMARAN/asgoto": {
			Branches: map[string]string{"feat/shared-pr-cache": "https://github.com/asumaran/asgoto/pull/42", "fix/old-thing": ""},
			Pulls:    map[string]sharedPR{"https://github.com/asumaran/asgoto/pull/42": {Repo: "asumaran/asgoto", Number: 42, State: "open"}},
		},
	}, now)
	if pr, _, at := s.branch("asumaran/asgoto", "feat/shared-pr-cache"); pr == nil || pr.Number != 42 || !at.Equal(now) {
		t.Errorf("the refreshed branch: %+v %v", pr, at)
	}
	if _, ok := s.pull("https://github.com/asumaran/asgoto/pull/41"); ok {
		t.Error("a PR no branch points to any more is dropped")
	}
	if pr, checked, _ := s.branch("me/bubbletea", "fix-x"); !checked || pr == nil {
		t.Error("a repo this refresh did not cover is kept")
	}
	if _, ok := s.Branches["old/repo"]; ok {
		t.Error("an entry not checked for a month is dropped")
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if got := sharedPRFile(); filepath.Base(filepath.Dir(got)) != "asumaran.asmeta" {
		t.Errorf("file = %s, want asmeta's state dir", got)
	}
	back := loadSharedPRs()
	if pr, _, _ := back.branch("asumaran/asgoto", "feat/shared-pr-cache"); pr == nil || pr.Number != 42 {
		t.Errorf("round trip: %+v", pr)
	}
	if (sharedPRs{}).save() == nil {
		t.Error("an incomplete document must not be written")
	}
}

func TestLockSharedPRs(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(t.TempDir(), "plugins", "asumaran.astool"))
	unlock, err := lockSharedPRs(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockSharedPRs(200 * time.Millisecond); err == nil {
		t.Error("a second writer must wait and give up while the lock is held")
	}
	unlock()
	again, err := lockSharedPRs(time.Second)
	if err != nil {
		t.Fatalf("the lock is free again: %v", err)
	}
	again()
	if _, err := os.Stat(filepath.Join(filepath.Dir(sharedPRFile()), "prs.lock")); err != nil {
		t.Error(err)
	}
}

func sharedPRPtr(s string) *string { return &s }
