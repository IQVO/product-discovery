package productview

import (
	"fmt"
	"strings"

	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// Outcome says whether a fact changed the row.
type Outcome int

// The two outcomes of a fold.
const (
	// Applied: the fact changed the row.
	Applied Outcome = iota
	// Stale: the per-source guard (or an already-known state) made the fact a
	// no-op; the consumer acknowledges it without effect.
	Stale
)

// MaxTitle bounds the item master title.
const MaxTitle = 500

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", shared.ErrInvalidFact, fmt.Sprintf(format, a...))
}

// ItemCreated is CatalogItemCreated: it completes the row (or a stub).
type ItemCreated struct {
	Title   string
	Hazmat  bool
	Fragile bool
}

// ApplyItemCreated completes a stub: title, flags and, unless a discontinue
// already arrived first, state ACTIVE. A row the item master already announced
// is left alone (the title is immutable there).
func (v View) ApplyItemCreated(f ItemCreated) (View, Outcome, error) {
	title := strings.TrimSpace(f.Title)
	if title == "" || len([]rune(title)) > MaxTitle {
		return v, Stale, invalid("item %s: title must be 1 to %d characters", v.CatalogItemID, MaxTitle)
	}
	if v.Title != "" {
		return v, Stale, nil
	}
	v.Title, v.Hazmat, v.Fragile = title, f.Hazmat, f.Fragile
	if v.Item == ItemStub {
		v.Item = ItemActive
	}
	return v, Applied, nil
}

// ApplyItemDiscontinued marks the item DISCONTINUED (never purchasable
// again). On a stub it records the state so a late create cannot revive it.
func (v View) ApplyItemDiscontinued() (View, Outcome) {
	if v.Item == ItemDiscontinued {
		return v, Stale
	}
	v.Item = ItemDiscontinued
	return v, Applied
}
