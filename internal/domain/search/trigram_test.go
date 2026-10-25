package search_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

func TestTokensSplitOnNonAlphanumerics(t *testing.T) {
	got := search.Tokens("  Red-Kettle, 1.7L! ")
	want := []string{"red", "kettle", "1", "7l"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokens = %v, want %v", got, want)
	}
	if len(search.Tokens("  ,; ")) != 0 {
		t.Fatal("punctuation only must give no token")
	}
}

func TestSimilarityMatchesPgTrgmReference(t *testing.T) {
	// The pg_trgm documentation's reference value.
	if got := search.Similarity("word", "two words"); math.Abs(got-0.363636) > 1e-5 {
		t.Fatalf("Similarity(word, two words) = %v, want 0.363636", got)
	}
	if got := search.Similarity("Kettle", "kettle"); got != 1 {
		t.Fatalf("case must not matter, got %v", got)
	}
	if got := search.Similarity("", "kettle"); got != 0 {
		t.Fatalf("empty text has no trigram, got %v", got)
	}
	if got := search.Similarity("kettle", ""); got != 0 {
		t.Fatalf("empty query has no trigram, got %v", got)
	}
}

func TestSimilarCoversTypos(t *testing.T) {
	if !search.Similar("Stainless Kettle", "kettel") {
		t.Fatal("a transposed typo must still match the title")
	}
	if search.Similar("Stainless Kettle", "bicycle") {
		t.Fatal("an unrelated word must not match")
	}
	// Exactly at the threshold counts as a match (>=).
	if !search.Similar("abc", "abc") {
		t.Fatal("identical text must match")
	}
}

func TestStemFoldsPlurals(t *testing.T) {
	cases := map[string]string{
		"kettles": "kettle", "batteries": "battery", "boxes": "box",
		"gas": "gas", "bus": "bus", "tea": "tea", "ies": "ies", "glass": "glas",
		"cats": "cat", "cars": "car", "bats": "bat", "lies": "lie", "ties": "tie", "pies": "pie", "taxes": "tax", "xes": "xes", "buses": "bus", "ches": "che", "cookies": "cooky",
		"fies": "fie", "dies": "die", "ses": "ses", "bees": "bee",
	}
	for in, want := range cases {
		if got := search.Stem(in); got != want {
			t.Errorf("Stem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoversAllNeedsEveryStem(t *testing.T) {
	if !search.CoversAll("Kitchen Kettles", "kettle kitchen") {
		t.Fatal("every query stem is in the name")
	}
	if search.CoversAll("Kettles", "kettle toaster") {
		t.Fatal("a missing stem must fail")
	}
	if search.CoversAll("Kettles", "  ") {
		t.Fatal("an empty query covers nothing")
	}
}

func TestStemSetIsSortedAndUnique(t *testing.T) {
	got := search.StemSet("Kettles kettle Alpha")
	if !reflect.DeepEqual(got, []string{"alpha", "kettle"}) {
		t.Fatalf("StemSet = %v", got)
	}
}

func TestSimilarityThresholdIsInclusive(t *testing.T) {
	// "ddfdba" and "ddfff" share 3 of 10 trigrams: exactly the 0.3 threshold.
	if got := search.Similarity("ddfdba", "ddfff"); got != search.SimilarityThreshold {
		t.Fatalf("fixture drifted: similarity = %v", got)
	}
	if !search.Similar("ddfdba", "ddfff") {
		t.Fatal("a similarity equal to the threshold matches")
	}
	// The same word inside a long title: the whole-title score is lower, the word score is exactly 0.3.
	if !search.Similar("ddfdba zzzzzzzz", "ddfff") {
		t.Fatal("a single word at the threshold matches")
	}
	if search.BestSimilarity("ddfdba zzzzzzzz", "ddfff") != search.SimilarityThreshold {
		t.Fatal("BestSimilarity takes the best word")
	}
}
