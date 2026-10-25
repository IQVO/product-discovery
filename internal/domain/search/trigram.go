package search

import (
	"sort"
	"strings"
	"unicode"
)

// SimilarityThreshold is the pg_trgm default of the % operator: a title is a
// typo-tolerant match when its trigram similarity to the query reaches it.
const SimilarityThreshold = 0.3

// Tokens splits text into lower-cased words (letters and digits); every other
// character separates words.
func Tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// trigrams returns the pg_trgm trigram set of text: per word, two leading
// blanks and one trailing blank are added, then every 3-character window.
func trigrams(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range Tokens(text) {
		padded := []rune("  " + w + " ")
		for i := 0; i+3 <= len(padded); i++ {
			set[string(padded[i:i+3])] = struct{}{}
		}
	}
	return set
}

// Similarity is the pg_trgm similarity of a and b: shared trigrams over all
// trigrams, in [0, 1]. similarity("word", "two words") is 0.363636...
func Similarity(a, b string) float64 {
	ta, tb := trigrams(a), trigrams(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	shared := 0
	for g := range ta {
		if _, ok := tb[g]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(ta)+len(tb)-shared)
}

// BestSimilarity is the larger of the trigram similarity of text to the whole
// title and to any single word of it, so a typo of one word of a long title
// still matches (the word_similarity side of the SQL adapter).
func BestSimilarity(title, text string) float64 {
	best := Similarity(title, text)
	for _, w := range Tokens(title) {
		best = max(best, Similarity(w, text))
	}
	return best
}

// Similar reports whether title matches text by trigram similarity.
func Similar(title, text string) bool {
	return BestSimilarity(title, text) >= SimilarityThreshold
}

// Stem strips a plural ending so "kettles" and "kettle" meet. It is the
// deliberately small category-name matcher; item text is stemmed by the
// full-text engine itself.
func Stem(token string) string {
	if len(token) > 4 && strings.HasSuffix(token, "ies") {
		return token[:len(token)-3] + "y"
	}
	if len(token) > 4 && hasSibilantPlural(token) {
		return token[:len(token)-2]
	}
	if len(token) > 3 && strings.HasSuffix(token, "s") {
		return token[:len(token)-1]
	}
	return token
}

// StemSet is the sorted, de-duplicated stems of the words of text.
func StemSet(text string) []string {
	seen := map[string]struct{}{}
	for _, t := range Tokens(text) {
		seen[Stem(t)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// CoversAll reports whether every stem of query occurs among the stems of
// name (and query has at least one): "kettles" is covered by "Kettle".
func CoversAll(name, query string) bool {
	want := StemSet(query)
	if len(want) == 0 {
		return false
	}
	have := map[string]struct{}{}
	for _, s := range StemSet(name) {
		have[s] = struct{}{}
	}
	for _, s := range want {
		if _, ok := have[s]; !ok {
			return false
		}
	}
	return true
}

func hasSibilantPlural(token string) bool {
	for _, end := range []string{"ses", "xes", "zes", "ches", "shes"} {
		if strings.HasSuffix(token, end) {
			return true
		}
	}
	return false
}
