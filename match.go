package main

// The fuzzy matcher is greedy: for every rune of the query it takes the first
// candidate it finds, left to right. Asked for "indexable" in "site orange tv
// indexable" it settles for the i of "site" and the n of "orange" before it
// gets to "dexable", so the highlight looks broken and the row scores as a
// scattered match. findTight corrects that: when the query occurs in one
// piece, that occurrence is the match.
//
// This file is the same in every tool of the family.

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"
)

// findTight is fuzzy.Find with every match tightened, best score first.
func findTight(q string, corpus []string) fuzzy.Matches {
	ms := fuzzy.Find(q, corpus)
	for i := range ms {
		tighten(q, &ms[i])
	}
	sort.Stable(ms)
	return ms
}

// tighten moves a match onto a contiguous occurrence of q in its string, if
// there is one, and scores it as such. MatchedIndexes stay byte offsets.
func tighten(q string, mt *fuzzy.Match) {
	pos := occurrence(strings.ToLower(mt.Str), strings.ToLower(q))
	if pos < 0 || pos >= len(mt.Str) {
		return
	}
	sub := fuzzy.Find(q, []string{mt.Str[pos:]})
	if len(sub) == 0 {
		return
	}
	idx := make([]int, len(sub[0].MatchedIndexes))
	for i, o := range sub[0].MatchedIndexes {
		idx[i] = pos + o
	}
	mt.MatchedIndexes = idx
	// The matcher scored the tail as if it were the whole string: charge the
	// skipped head the way it would (a point per rune, five per leading rune
	// up to fifteen) and take back the first-rune bonus a mid-word start has
	// not earned.
	head := utf8.RuneCountInString(mt.Str[:pos])
	score := sub[0].Score - head - min(15, 5*head)
	if pos > 0 && !atWordStart(mt.Str, pos) {
		score -= 10
	}
	mt.Score = max(mt.Score, score)
}

// occurrence is the byte offset of q in s: the first one at the start of a
// word, else the first one, else -1.
func occurrence(s, q string) int {
	if q == "" {
		return -1
	}
	first := -1
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], q)
		if i < 0 {
			break
		}
		i += from
		if first < 0 {
			first = i
		}
		if atWordStart(s, i) {
			return i
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
	return first
}

func atWordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
