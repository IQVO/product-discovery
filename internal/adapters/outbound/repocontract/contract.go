// Package repocontract is the repository contract suite: the behaviours every
// persistence adapter (the in-memory doubles and Postgres) must show. It is
// test support (it imports "testing") kept out of _test.go files so both
// adapters can run it; the shipped binary never links it (enforced by
// internal/architecture).
package repocontract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/application/ports"
)

// Env is what an adapter hands to the suite. Every test gets a fresh Env.
type Env struct {
	Products   ports.ProductRepository
	Categories ports.CategoryRepository
	Listings   ports.ListingItemMap
	Inbox      ports.Inbox
	UoW        ports.UnitOfWork
	Search     ports.SearchPort
}

// Run executes the whole suite; newEnv must return an empty Env each time.
func Run(t *testing.T, newEnv func(t *testing.T) Env) {
	t.Helper()
	suites := map[string]func(*testing.T, Env){
		"products":    testProducts,
		"purchasable": testPurchasableCounts,
		"categories":  testCategories,
		"listings":    testListings,
		"inbox":       testInbox,
		"unitofwork":  testUnitOfWork,
		"search":      testSearch,
	}
	for name, fn := range suites {
		t.Run(name, func(t *testing.T) { fn(t, newEnv(t)) })
	}
}

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

var bg = context.Background

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
