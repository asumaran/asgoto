package main

import (
	"testing"

	"github.com/sahilm/fuzzy"
)

func contiguous(idx []int) bool {
	for i := 1; i < len(idx); i++ {
		if idx[i] != idx[i-1]+1 {
			return false
		}
	}
	return len(idx) > 0
}

func TestFindTightPrefersTheWholeWord(t *testing.T) {
	s := "site orange tv indexable"
	loose := fuzzy.Find("indexable", []string{s})[0]
	if contiguous(loose.MatchedIndexes) {
		t.Skip("the matcher is no longer greedy; findTight may be unnecessary")
	}
	got := findTight("indexable", []string{s})[0]
	if !contiguous(got.MatchedIndexes) || got.MatchedIndexes[0] != 15 || len(got.MatchedIndexes) != 9 {
		t.Errorf("idx = %v, want the 9 bytes from 15", got.MatchedIndexes)
	}
	if got.Score < loose.Score {
		t.Errorf("score %d < the scattered match's %d", got.Score, loose.Score)
	}
}

func TestFindTightRanksTheWholeWordFirst(t *testing.T) {
	corpus := []string{"coach emails", "a cat in the cache"}
	if loose := fuzzy.Find("cache", corpus); len(loose) != 2 || loose[0].Index != 0 {
		t.Fatalf("the fixture no longer fools the plain matcher: %+v", loose)
	}
	ms := findTight("cache", corpus)
	if len(ms) != 2 || ms[0].Index != 1 || !contiguous(ms[0].MatchedIndexes) {
		t.Errorf("ranking = %+v, want the row that says cache first", ms)
	}
}

func TestFindTightPicksAWordStartAndKeepsByteOffsets(t *testing.T) {
	got := findTight("log", []string{"catálogo: login"})[0] // "log" sits inside catálogo too
	want := len("catálogo: ")
	if got.MatchedIndexes[0] != want || !contiguous(got.MatchedIndexes) {
		t.Errorf("idx = %v, want byte offset %d (the word login)", got.MatchedIndexes, want)
	}
	if got := findTight("LOG", []string{"Login flow"})[0]; got.MatchedIndexes[0] != 0 {
		t.Errorf("the occurrence is found whatever the case: %v", got.MatchedIndexes)
	}
}

func TestFindTightLeavesScatteredMatchesAlone(t *testing.T) {
	loose := fuzzy.Find("fxlg", []string{"fix login"})
	got := findTight("fxlg", []string{"fix login"})
	if len(got) != 1 || got[0].Score != loose[0].Score || len(got[0].MatchedIndexes) != 4 {
		t.Errorf("got %+v, want %+v untouched", got, loose)
	}
	if len(findTight("zzz", []string{"fix login"})) != 0 {
		t.Errorf("no match stays no match")
	}
}
