package main

// The fuzzy matcher is greedy: for every rune of the query it takes the first
// candidate it finds, left to right. Asked for "indexable" in "site orange tv
// indexable" it settles for the i of "site" and the n of "orange" before it
// gets to "dexable", so the highlight looks broken and the row scores as a
// scattered match. findTight corrects that: when the query occurs in one
// piece, that occurrence is the match.
//
// It also charges a point for every byte of the string the query did not
// match, so of two strings that match equally well the shorter one wins. The
// lists here have an order of their own (newest first) that should decide
// between equals, not the length of a title: findTight gives that charge
// back, and a score says only how good the match is.
//
// A query is split on whitespace and every term must match, in any order and
// in any of the fields an item is searched by. A term is fuzzy in the pickers
// and a substring in asgitlog (its list keeps the log's order, so nothing
// would push a scattered match down); ~term asks for fuzzy and 'term for a
// substring wherever the other one is the default.
//
// This file is the same in every tool of the family.

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"
)

// qterm is one term of a query. text is lowercased unless the term is fuzzy
// (the fuzzy matcher folds case itself).
type qterm struct {
	text  string
	fuzzy bool
}

// queryTerms splits q into its terms. fuzzyByDefault says what a term without
// a prefix is; a prefix standing alone is not a term yet.
func queryTerms(q string, fuzzyByDefault bool) []qterm {
	var terms []qterm
	for _, f := range strings.Fields(q) {
		isFuzzy := fuzzyByDefault
		if rest, ok := strings.CutPrefix(f, "~"); ok {
			f, isFuzzy = rest, true
		} else if rest, ok := strings.CutPrefix(f, "'"); ok {
			f, isFuzzy = rest, false
		}
		if f == "" {
			continue
		}
		if !isFuzzy {
			f = strings.ToLower(f)
		}
		terms = append(terms, qterm{text: f, fuzzy: isFuzzy})
	}
	return terms
}

// fieldsHit is what a query found in one item. Field is the field the
// best-scoring term matched in. Idx holds, per field, the byte offsets matched
// by the terms that did best in that field; Any holds the offsets of every
// term that matched there at all, best field or not. Both are sorted.
type fieldsHit struct {
	Score int
	Field int
	Idx   [][]int
	Any   [][]int
}

// findFields matches q against items searched by several parallel fields
// (fields[f][i] is field f of item i) and returns the items every term matched
// in some field, keyed by item. A term scores what its best field scored, the
// earlier field winning a tie, and an item scores the sum of its terms. A
// query with no terms yet matches every item.
func findFields(q string, fields ...[]string) map[int]fieldsHit {
	hits := map[int]fieldsHit{}
	if len(fields) == 0 {
		return hits
	}
	blank := func() fieldsHit {
		return fieldsHit{Idx: make([][]int, len(fields)), Any: make([][]int, len(fields))}
	}
	terms := queryTerms(q, true)
	if len(terms) == 0 {
		for i := range fields[0] {
			hits[i] = blank()
		}
		return hits
	}
	type termBest struct{ score, field int }
	top := map[int]int{} // the best term score seen per item, for Field
	for n, t := range terms {
		best := map[int]termBest{}
		found := map[int]fieldsHit{}
		for f, corpus := range fields {
			for _, mt := range findTerm(t, corpus) {
				h, ok := found[mt.Index]
				if !ok {
					h = blank()
				}
				h.Any[f] = mt.MatchedIndexes
				if b, seen := best[mt.Index]; !seen || mt.Score > b.score {
					best[mt.Index] = termBest{mt.Score, f}
				}
				found[mt.Index] = h
			}
		}
		next := map[int]fieldsHit{}
		for i, got := range found {
			h, ok := hits[i]
			if n == 0 {
				h, ok = blank(), true
			}
			if !ok {
				continue // an earlier term did not match this item
			}
			b := best[i]
			h.Score += b.score
			if top[i] < b.score || n == 0 {
				top[i], h.Field = b.score, b.field
			}
			h.Idx[b.field] = mergeIdx(h.Idx[b.field], got.Any[b.field])
			for f := range fields {
				h.Any[f] = mergeIdx(h.Any[f], got.Any[f])
			}
			next[i] = h
		}
		hits = next
	}
	return hits
}

// findTerm is findTight for one term: a term that is not fuzzy keeps only the
// strings it occurs in, which tighten has already moved the match onto.
func findTerm(t qterm, corpus []string) fuzzy.Matches {
	ms := findTight(t.text, corpus)
	if t.fuzzy {
		return ms
	}
	kept := ms[:0]
	for _, mt := range ms {
		if strings.Contains(strings.ToLower(mt.Str), t.text) {
			kept = append(kept, mt)
		}
	}
	return kept
}

// mergeIdx is the sorted union of two sets of offsets.
func mergeIdx(a, b []int) []int {
	if len(b) == 0 {
		return a
	}
	seen := make(map[int]bool, len(a)+len(b))
	out := make([]int, 0, len(a)+len(b))
	for _, s := range [][]int{a, b} {
		for _, i := range s {
			if !seen[i] {
				seen[i] = true
				out = append(out, i)
			}
		}
	}
	sort.Ints(out)
	return out
}

// findTight is fuzzy.Find with every match tightened and its length charge
// given back, best score first.
func findTight(q string, corpus []string) fuzzy.Matches {
	ms := fuzzy.Find(q, corpus)
	for i := range ms {
		ms[i].Score += unmatched(ms[i])
		tighten(q, &ms[i])
	}
	// equal scores: the order the strings came in, not the one the matcher
	// left them in before the scores were corrected
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Score != ms[j].Score {
			return ms[i].Score > ms[j].Score
		}
		return ms[i].Index < ms[j].Index
	})
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
	// skipped head the way it would (five per leading rune up to fifteen) and
	// take back the first-rune bonus a mid-word start has not earned.
	head := utf8.RuneCountInString(mt.Str[:pos])
	score := sub[0].Score + unmatched(sub[0]) - min(15, 5*head)
	if pos > 0 && !atWordStart(mt.Str, pos) {
		score -= 10
	}
	mt.Score = max(mt.Score, score)
}

// unmatched is what the matcher charged m for its length: a point for every
// byte of the string that is not part of the match.
func unmatched(m fuzzy.Match) int { return len(m.Str) - len(m.MatchedIndexes) }

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

// hasTerms reports whether a query searches for anything. Spaces and a bare
// `~` or `'` do not, so they must not filter, rank or move the cursor: every
// tool asks this instead of comparing the input with "".
func hasTerms(q string) bool { return len(queryTerms(q, true)) > 0 }
