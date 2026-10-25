package search_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

func i64(v int64) *int64 { return &v }

func TestNewQueryDefaults(t *testing.T) {
	q, err := search.NewQuery(search.Input{Text: "  red   kettle ", CategoryID: " c1 "})
	if err != nil {
		t.Fatal(err)
	}
	if q.Text != "red kettle" || q.CategoryID != "c1" || q.Sort != search.SortRelevance || q.Limit != search.DefaultLimit {
		t.Fatalf("query = %+v", q)
	}
	if q.Browse() || !(search.Query{}).Browse() {
		t.Fatal("Browse must mean no text")
	}
	if q.HasPriceFilter() {
		t.Fatal("no price bound set")
	}
}

func TestNewQueryRejects(t *testing.T) {
	bad := map[string]search.Input{
		"q":        {Text: strings.Repeat("x", search.MaxTextLength+1)},
		"sort":     {Sort: "cheapest"},
		"limit":    {Limit: search.MaxLimit + 1},
		"priceMin": {PriceMin: i64(-1)},
		"priceMax": {PriceMax: i64(-5)},
		"after":    {After: "!!"},
	}
	for field, in := range bad {
		_, err := search.NewQuery(in)
		var ve *search.ValidationError
		if !errors.Is(err, search.ErrValidation) || !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: err = %v", field, err)
		}
	}
	if _, err := search.NewQuery(search.Input{Limit: -1}); !errors.Is(err, search.ErrValidation) {
		t.Errorf("negative limit: %v", err)
	}
	_, err := search.NewQuery(search.Input{PriceMin: i64(10), PriceMax: i64(5)})
	if !errors.Is(err, search.ErrValidation) {
		t.Errorf("min > max: %v", err)
	}
}

func TestNewQueryAcceptsBoundaries(t *testing.T) {
	q, err := search.NewQuery(search.Input{
		Text: strings.Repeat("é", search.MaxTextLength), Limit: search.MaxLimit,
		PriceMin: i64(5), PriceMax: i64(5),
	})
	if err != nil || !q.HasPriceFilter() || q.Limit != search.MaxLimit {
		t.Fatalf("boundary query: %+v %v", q, err)
	}
	if _, err := search.NewQuery(search.Input{Limit: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestCursorRoundTripAndSortBinding(t *testing.T) {
	c := search.Cursor{Sort: search.SortPriceAsc, Mode: search.ModeFTS, Price: 1200, ID: "I-1"}
	got, err := search.DecodeCursor(c.Encode())
	if err != nil || got != c {
		t.Fatalf("round trip = %+v %v", got, err)
	}
	if _, err := search.NewQuery(search.Input{Sort: "title", After: c.Encode()}); !errors.Is(err, search.ErrValidation) {
		t.Fatalf("a cursor of another sort must be refused: %v", err)
	}
	q, err := search.NewQuery(search.Input{Sort: "price-asc", After: c.Encode()})
	if err != nil || q.After == nil || q.After.ID != "I-1" {
		t.Fatalf("matching cursor: %+v %v", q, err)
	}
}

func TestDecodeCursorRejectsJunk(t *testing.T) {
	for _, s := range []string{"", "%%%", "e30", search.Cursor{ID: "x", Mode: "weird"}.Encode(), search.Cursor{Mode: search.ModeFTS}.Encode()} {
		if _, err := search.DecodeCursor(s); !errors.Is(err, search.ErrBadCursor) {
			t.Errorf("DecodeCursor(%q) = %v", s, err)
		}
	}
}
