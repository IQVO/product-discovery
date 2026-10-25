// Package search holds the value objects of the product search: the validated
// Query, the keyset Cursor, the price Bands, the Facets and the trigram
// similarity the typo fallback uses. It is pure: no I/O, no clock.
//
// The search itself runs behind the SearchPort of the application layer
// (PostgreSQL full-text search plus pg_trgm today); this package is the part
// of the behaviour that does not depend on the engine.
package search

import (
	"errors"
	"fmt"
	"strings"
)

// Sort is the ordering of a search.
type Sort string

// The four orderings of GET /products.
const (
	SortRelevance Sort = "relevance"
	SortPriceAsc  Sort = "price-asc"
	SortPriceDesc Sort = "price-desc"
	SortTitle     Sort = "title"
)

// Page sizes: DefaultLimit applies when the caller sends none.
const (
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxTextLength bounds the free-text query (characters).
	MaxTextLength = 200
)

// ErrValidation is the sentinel every ValidationError matches (problem type
// validation-failed, HTTP 422).
var ErrValidation = errors.New("validation failed")

// ValidationError names the offending query field.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Reason) }

// Is makes errors.Is(err, ErrValidation) true.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }

// Input is the raw, parsed-but-unchecked form of a search request. A nil
// pointer means "parameter absent".
type Input struct {
	Text       string
	CategoryID string
	InStock    *bool
	PriceMin   *int64
	PriceMax   *int64
	Sort       string
	After      string
	Limit      int
}

// Query is a validated search request.
type Query struct {
	Text       string
	CategoryID string
	InStock    *bool
	PriceMin   *int64
	PriceMax   *int64
	Sort       Sort
	After      *Cursor
	Limit      int
}

// NewQuery validates in. An empty sort is relevance, a zero limit is
// DefaultLimit; everything else out of range is a ValidationError.
func NewQuery(in Input) (Query, error) {
	q := Query{
		Text:       strings.Join(strings.Fields(in.Text), " "),
		CategoryID: strings.TrimSpace(in.CategoryID),
		InStock:    in.InStock,
		PriceMin:   in.PriceMin,
		PriceMax:   in.PriceMax,
		Limit:      in.Limit,
	}
	if len([]rune(q.Text)) > MaxTextLength {
		return Query{}, invalid("q", fmt.Sprintf("at most %d characters", MaxTextLength))
	}
	sort, err := parseSort(in.Sort)
	if err != nil {
		return Query{}, err
	}
	q.Sort = sort
	if err := checkLimit(&q); err != nil {
		return Query{}, err
	}
	if err := checkPrices(q); err != nil {
		return Query{}, err
	}
	if in.After != "" {
		c, err := DecodeCursor(in.After)
		if err != nil || c.Sort != q.Sort {
			return Query{}, invalid("after", "not a cursor of this search")
		}
		q.After = &c
	}
	return q, nil
}
