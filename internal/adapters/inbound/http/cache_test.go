package http_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// getWith is e.get with one extra request header set.
func getWith(e *env, path, header, value string) reply {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, strings.NewReader(""))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set(header, value)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: string(b)}
}

// reprice moves the item's price to a new version, changing the read.
func reprice(e *env, id string, version int64) {
	e.t.Helper()
	e.must(e.consumer.ApplyItemPriceChanged(context.Background(), e.meta(), usecases.ItemPriceChanged{
		CatalogItemID: id, Price: productview.PriceChanged{
			Currency: "USD", ListPriceMinor: 9900, EffectivePriceMinor: 8900, Version: version,
		},
	}))
}

func TestProductReadCarriesETagAndCacheControl(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	r := e.get("/products/I1")
	e.wantStatus(r, http.StatusOK)
	etag := r.Header.Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || len(etag) < 3 {
		t.Fatalf("ETag = %q, want a quoted non-empty strong tag", etag)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "max-age=30" {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestProductReadAnswers304OnMatchingIfNoneMatch(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	etag := e.get("/products/I1").Header.Get("ETag")
	r := getWith(e, "/products/I1", "If-None-Match", etag)
	e.wantStatus(r, http.StatusNotModified)
	if r.Body != "" {
		t.Fatalf("304 body = %q, want empty", r.Body)
	}
	if got := r.Header.Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
}

func TestProductReadHonoursWeakAndStarIfNoneMatch(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	etag := e.get("/products/I1").Header.Get("ETag")
	e.wantStatus(getWith(e, "/products/I1", "If-None-Match", "W/"+etag), http.StatusNotModified)
	e.wantStatus(getWith(e, "/products/I1", "If-None-Match", `*`), http.StatusNotModified)
	stale := getWith(e, "/products/I1", "If-None-Match", `"0000000000000000"`)
	e.wantStatus(stale, http.StatusOK)
	if !strings.Contains(stale.Body, "Steel Kettle") {
		t.Fatalf("200 after a stale If-None-Match lost the body: %q", stale.Body)
	}
}

func TestProductETagChangesWhenTheViewChanges(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	before := e.get("/products/I1").Header.Get("ETag")
	reprice(e, "I1", 2)
	after := e.get("/products/I1").Header.Get("ETag")
	if before == "" || after == "" || before == after {
		t.Fatalf("ETag did not change across a price version bump: %q -> %q", before, after)
	}
	// The tag held before the bump no longer matches: a full 200, not 304.
	e.wantStatus(getWith(e, "/products/I1", "If-None-Match", before), http.StatusOK)
}

func TestCategoryReadCarriesETagAnd304(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	r := e.get("/categories")
	e.wantStatus(r, http.StatusOK)
	etag := r.Header.Get("ETag")
	if etag == "" || r.Header.Get("Cache-Control") != "max-age=30" {
		t.Fatalf("headers: ETag=%q Cache-Control=%q", etag, r.Header.Get("Cache-Control"))
	}
	nm := getWith(e, "/categories", "If-None-Match", etag)
	e.wantStatus(nm, http.StatusNotModified)
	if nm.Body != "" {
		t.Fatalf("304 body = %q, want empty", nm.Body)
	}
}
