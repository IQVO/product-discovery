package shared_test

import (
	"strings"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func TestValidID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"item-004512", true},
		{"A", true},
		{"0", true},
		{"a.b_c:d-e", true},
		{strings.Repeat("a", shared.MaxID), true},
		{strings.Repeat("a", shared.MaxID+1), false},
		{"", false},
		{"-a", false},
		{".a", false},
		{"_a", false},
		{":a", false},
		{"a b", false},
		{"a/b", false},
		{"é", false},
		{"a\x00", false},
	}
	for _, c := range cases {
		if got := shared.ValidID(c.id); got != c.want {
			t.Errorf("ValidID(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestEveryAlnumRangeBoundaryIsHandled(t *testing.T) {
	for _, id := range []string{"a", "z", "A", "Z", "0", "9"} {
		if !shared.ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"`", "{", "@", "[", "/", ":x"} {
		if shared.ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}
