package search_test

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

func TestDefaultBandsAndLabels(t *testing.T) {
	bands := search.Bands(search.DefaultBandEdges)
	want := []string{"<2500", "2500-4999", "5000-9999", "10000-24999", "25000+"}
	if len(bands) != len(want) {
		t.Fatalf("got %d bands, want %d", len(bands), len(want))
	}
	for i, b := range bands {
		if b.Label() != want[i] {
			t.Errorf("band %d label = %q, want %q", i, b.Label(), want[i])
		}
	}
}

func TestBandOfEdges(t *testing.T) {
	bands := search.Bands(search.DefaultBandEdges)
	cases := map[int64]string{
		0: "<2500", 2499: "<2500", 2500: "2500-4999", 4999: "2500-4999", 5000: "5000-9999",
		9999: "5000-9999", 10000: "10000-24999", 24999: "10000-24999", 25000: "25000+", 9999999: "25000+",
	}
	for price, want := range cases {
		if got := search.BandOf(bands, price).Label(); got != want {
			t.Errorf("BandOf(%d) = %q, want %q", price, got, want)
		}
	}
}

func TestBandsIgnoreBadEdges(t *testing.T) {
	got := search.Bands([]int64{100, 50, 200})
	if len(got) != 2 || got[0].Upper != 100 || got[1].Lower != 100 || got[1].Upper != 0 {
		t.Fatalf("Bands with a descending edge = %+v", got)
	}
	if got := search.Bands(nil); len(got) != 1 || got[0].Label() != "0+" {
		t.Fatalf("Bands(nil) = %+v", got)
	}
	if got := search.Bands([]int64{0, 10}); len(got) != 1 {
		t.Fatalf("a zero edge must be ignored, got %+v", got)
	}
}
