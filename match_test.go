package main

import (
	"reflect"
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
	if len(got) != 1 || len(got[0].MatchedIndexes) != 4 || got[0].MatchedIndexes[0] != loose[0].MatchedIndexes[0] {
		t.Errorf("got %+v, want the scattered match %+v", got, loose)
	}
	if len(findTight("zzz", []string{"fix login"})) != 0 {
		t.Errorf("no match stays no match")
	}
}

func TestFindTightDoesNotRewardShortStrings(t *testing.T) {
	corpus := []string{"eshop-2707", "eshop-551", "eshop-2707 structured data for every page"}
	if loose := fuzzy.Find("eshop", corpus); loose[0].Index != 1 {
		t.Fatalf("the plain matcher no longer prefers the shorter string: %+v", loose)
	}
	ms := findTight("eshop", corpus)
	if len(ms) != 3 || ms[0].Score != ms[1].Score || ms[1].Score != ms[2].Score {
		t.Fatalf("the same match scores the same whatever the length: %+v", ms)
	}
	if ms[0].Index != 0 || ms[1].Index != 1 || ms[2].Index != 2 {
		t.Errorf("equal scores keep the order they came in: %+v", ms)
	}
}

func TestQueryTerms(t *testing.T) {
	got := queryTerms("  Fix ~LoGin 'Cart ' ~ ", true)
	want := []qterm{{"Fix", true}, {"LoGin", true}, {"cart", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fuzzy by default: %+v, want %+v", got, want)
	}
	got = queryTerms("Fix ~LoGin 'Cart", false)
	want = []qterm{{"fix", false}, {"LoGin", true}, {"cart", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("substring by default: %+v, want %+v", got, want)
	}
}

func TestFindFieldsTermsInAnyOrderAcrossFields(t *testing.T) {
	titles := []string{"fix login flow", "update readme", "login page copy"}
	branches := []string{"fix/login", "docs/readme", "feat/copy"}
	for _, q := range []string{"login fix", "fix login"} {
		hits := findFields(q, titles, branches)
		if len(hits) != 1 {
			t.Fatalf("%q: hits %v, want only item 0", q, hits)
		}
		if h := hits[0]; !reflect.DeepEqual(h.Idx[0], []int{0, 1, 2, 4, 5, 6, 7, 8}) {
			t.Errorf("%q: title offsets %v, want both words", q, h.Idx[0])
		}
	}
	// a term may match one field and the next another
	hits := findFields("readme docs", titles, branches)
	if h, ok := hits[1]; len(hits) != 1 || !ok || len(h.Idx[0]) != 6 || len(h.Idx[1]) != 4 {
		t.Errorf("terms across fields: %+v", hits)
	}
	one, two := findFields("login", titles, branches)[0], findFields("login fix", titles, branches)[0]
	if two.Score <= one.Score {
		t.Errorf("a second matching term must add to the score: %d then %d", one.Score, two.Score)
	}
}

func TestFindFieldsExactTermAndBarePrefix(t *testing.T) {
	titles := []string{"site orange tv indexable", "indexable pages", "in the dex table"}
	if hits := findFields("idxbl", titles); len(hits) != 3 {
		t.Errorf("fuzzy term: %v, want every string holding the letters in order", hits)
	}
	if hits := findFields("'dex", titles); len(hits) != 3 {
		t.Errorf("'dex: %v, want every string holding dex", hits)
	}
	if hits := findFields("'idxbl", titles); len(hits) != 0 {
		t.Errorf("'idxbl occurs nowhere in one piece: %v", hits)
	}
	if hits := findFields("'", titles); len(hits) != 3 {
		t.Errorf("a bare prefix is not a term yet and must not empty the list: %v", hits)
	}
	if hits := findFields("pages '", titles); len(hits) != 1 {
		t.Errorf("a bare prefix after a term: %v", hits)
	}
}
