//go:build integration

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}

// upstream serves one fixed snapshot per host.
func upstream(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRebuildSeedsARealDatabaseAndIsRepeatable(t *testing.T) {
	oa := upstream(t, map[string]string{
		"/catalog":  `{"items":[{"catalogItemId":"I1","title":"Steel Kettle","state":"ACTIVE"}]}`,
		"/listings": `{"listings":[{"listingId":"L1","catalogItemId":"I1","nodeId":"N","state":"LIVE","availability":{"availableQty":3,"asOf":"2026-10-25T09:00:00Z"}}]}`,
	})
	pr := upstream(t, map[string]string{"/item-prices": `{"items":[{"catalogItemId":"I1","state":"ACTIVE","listPrice":{"amountMinor":900,"currency":"BRL"},"effectivePrice":{"amountMinor":800,"currency":"BRL"},"version":3}]}`})
	pc := upstream(t, map[string]string{
		"/product-contents": `{"items":[{"catalogItemId":"I1","state":"PUBLISHED","revision":1,"description":"d","primaryCategoryId":"c1"}]}`,
		"/categories":       `{"version":2,"categories":[{"categoryId":"c1","name":"One","path":"One"}]}`,
	})
	url := pgtest.EmptyURL(t)
	env := map[string]string{"DATABASE_URL": url, "OFFER_AVAILABILITY_URL": oa, "PRICING_URL": pr, "PRODUCT_CONTENT_URL": pc}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := 0; i < 2; i++ { // the second run must change nothing and fail nothing
		if err := run(context.Background(), envOf(env), logger); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	pool, err := postgres.NewPool(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	v, err := postgres.NewProductRepository(pool).Get(context.Background(), "I1")
	if err != nil || !v.Purchasable() || v.EffectivePriceMinor != 800 || v.PriceVersion != 3 || v.CategoryID != "c1" {
		t.Fatalf("view = %+v %v", v, err)
	}
	cats, _ := postgres.NewCategoryRepository(pool).Current(context.Background())
	if cats.Version() != 2 {
		t.Fatalf("tree version = %d", cats.Version())
	}
}
