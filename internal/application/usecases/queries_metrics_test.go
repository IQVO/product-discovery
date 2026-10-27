package usecases_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// recordingMetrics lists the ports.SearchMetrics calls of a use case.
type recordingMetrics struct {
	mu    sync.Mutex
	calls []string
}

var _ ports.SearchMetrics = (*recordingMetrics)(nil)

func (m *recordingMetrics) add(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, s)
}

func (m *recordingMetrics) SearchServed(context.Context) { m.add("served") }
func (m *recordingMetrics) SearchEmpty(context.Context)  { m.add("empty") }
func (m *recordingMetrics) SearchFailed(_ context.Context, reason string) {
	m.add("failed/" + reason)
}

// brokenCategories fails every read of the category tree.
type brokenCategories struct{ ports.CategoryRepository }

func (brokenCategories) Current(context.Context) (categoryview.View, error) {
	return categoryview.View{}, errors.New("database down")
}

func TestSearchProductsCountsEachCallOnceByOutcome(t *testing.T) {
	e := newEnv()
	m := &recordingMetrics{}
	e.deps.Metrics = m
	e.publishTree(t, 1)
	e.makePurchasable(t, "I1", "Steel Kettle", "kettles")

	var next string
	run := func(in search.Input) {
		page, _ := (usecases.Queries{Deps: e.deps}).SearchProducts(bg(), in)
		next = page.Next
	}
	run(search.Input{Sort: "title", Limit: 1})              // hits, one more page may follow
	run(search.Input{Sort: "title", Limit: 1, After: next}) // a continuation page is served even when it holds nothing
	run(search.Input{CategoryID: "nope"})                   // first page, nothing
	run(search.Input{Sort: "no-such-ordering"})             // rejected input
	e.deps.Categories = brokenCategories{e.deps.Categories}
	run(search.Input{}) // dependency failure

	want := []string{"served", "served", "empty", "failed/validation-failed", "failed/internal-error"}
	if !reflect.DeepEqual(m.calls, want) {
		t.Fatalf("metric calls = %v, want %v", m.calls, want)
	}
}

func TestSearchProductsWithoutMetricsRecordsNothing(t *testing.T) {
	e := newEnv()
	if _, err := e.queries.SearchProducts(bg(), search.Input{}); err != nil {
		t.Fatal(err)
	}
}
