package main

// Search that behaves like fzf's extended search, using fzf's own matching algorithm:
//   - space-separated terms must all match, in any order
//   - smart case: a term is case-sensitive only if it has an uppercase letter
//   - 'term matches exactly, ^term at the start, term$ at the end, !term excludes
//   - items are ordered by score, then by length, then by their original order (fzf's defaults)

import (
	"slices"
	"strings"
	"unicode"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

func init() { algo.Init("default") }

type matchFunc func(caseSensitive, normalize, forward bool, input *util.Chars, pattern []rune, withPos bool, slab *util.Slab) (algo.Result, *[]int)

type term struct {
	fn            matchFunc
	pattern       []rune
	caseSensitive bool
	inverse       bool
}

func parseTerms(query string) []term {
	var terms []term
	for _, word := range strings.Fields(query) {
		t := term{fn: algo.FuzzyMatchV2}
		if rest, ok := strings.CutPrefix(word, "!"); ok {
			// Like fzf, an excluded term is matched exactly.
			t.inverse, t.fn, word = true, algo.ExactMatchNaive, rest
		}
		switch {
		case strings.HasPrefix(word, "'"):
			t.fn, word = algo.ExactMatchNaive, word[1:]
		case strings.HasPrefix(word, "^"):
			t.fn, word = algo.PrefixMatch, word[1:]
		case strings.HasSuffix(word, "$") && len(word) > 1:
			t.fn, word = algo.SuffixMatch, word[:len(word)-1]
		}
		if word == "" {
			continue
		}
		t.caseSensitive = strings.ContainsFunc(word, unicode.IsUpper)
		t.pattern = []rune(word)
		if !t.caseSensitive {
			t.pattern = []rune(strings.ToLower(word))
		}
		t.pattern = algo.NormalizeRunes(t.pattern)
		terms = append(terms, t)
	}
	return terms
}

// Returns the items that match every term, best first, with the byte offsets to highlight.
func search(items []item, query string) []match {
	terms := parseTerms(query)
	type scored struct {
		match
		score, length, index int
	}
	var results []scored
	for i, it := range items {
		text := it.searchText()
		chars := util.ToChars([]byte(text))
		score, matched, ok := 0, map[int]bool{}, true
		for _, t := range terms {
			res, pos := t.fn(t.caseSensitive, true, true, &chars, t.pattern, true, nil)
			if found := res.Start >= 0; found == t.inverse {
				ok = false
				break
			}
			if t.inverse {
				continue
			}
			score += res.Score
			if pos != nil {
				for _, p := range *pos {
					matched[p] = true
				}
			} else {
				for p := res.Start; p < res.End; p++ {
					matched[p] = true
				}
			}
		}
		if !ok {
			continue
		}
		results = append(results, scored{match{it, byteOffsets(text, matched)}, score, chars.Length(), i})
	}
	slices.SortStableFunc(results, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if a.length != b.length {
			return a.length - b.length
		}
		return a.index - b.index
	})
	matches := make([]match, len(results))
	for i, r := range results {
		matches[i] = r.match
	}
	return matches
}

// fzf reports positions as character indexes; the list highlights by byte offset.
func byteOffsets(text string, chars map[int]bool) []int {
	var offsets []int
	i := 0
	for offset := range text {
		if chars[i] {
			offsets = append(offsets, offset)
		}
		i++
	}
	return offsets
}
