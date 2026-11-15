package search_test

import (
	"errors"
	"io"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

// TestValidationErrorMatchesInvalidCursorOnlyForTheCursorField pins
// ValidationError.Is: every validation error matches ErrValidation, but only
// the one about the `after` cursor also matches ErrInvalidCursor (HTTP maps
// that one to 400 invalid-cursor, every other field stays 422).
func TestValidationErrorMatchesInvalidCursorOnlyForTheCursorField(t *testing.T) {
	trgm := search.Cursor{Sort: search.SortTitle, Mode: search.ModeTrgm, ID: "I-1"}
	cases := []struct {
		name       string
		in         search.Input
		wantCursor bool
	}{
		{"undecodable cursor", search.Input{After: "!!"}, true},
		{"cursor of another search kind", search.Input{Sort: "title", After: trgm.Encode()}, true},
		{"limit out of range", search.Input{Limit: -1}, false},
		{"unknown sort", search.Input{Sort: "cheapest"}, false},
		{"inverted price range", search.Input{PriceMin: i64(10), PriceMax: i64(5)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := search.NewQuery(c.in)
			if !errors.Is(err, search.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if got := errors.Is(err, search.ErrInvalidCursor); got != c.wantCursor {
				t.Fatalf("errors.Is(err, ErrInvalidCursor) = %v, want %v (err = %v)", got, c.wantCursor, err)
			}
			if errors.Is(err, io.EOF) {
				t.Fatalf("a validation error must not match an unrelated sentinel: %v", err)
			}
		})
	}
}
