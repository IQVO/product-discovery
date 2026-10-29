package usecases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// flakyUoW fails the first n units with a retryable conflict.
type flakyUoW struct {
	ports.UnitOfWork
	left  int
	calls int
}

func (f *flakyUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	if f.left > 0 {
		f.left--
		return ports.ErrConcurrentUpdate
	}
	return f.UnitOfWork.Do(ctx, fn)
}

func TestConsumerRerunsAConflictedUnit(t *testing.T) {
	e := newEnv()
	flaky := &flakyUoW{UnitOfWork: e.deps.UoW, left: 2}
	d := e.deps
	d.UoW = flaky
	c := usecases.Consumer{Deps: d}
	must(t, c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: "I1", Title: "T"}))
	if flaky.calls != 3 || e.product(t, "I1").Title != "T" {
		t.Fatalf("calls = %d", flaky.calls)
	}
	flaky.left = 100
	flaky.calls = 0
	err := c.ApplyCatalogItemCreated(bg(), e.meta(), usecases.CatalogItemCreated{CatalogItemID: "I2", Title: "T"})
	if !errors.Is(err, ports.ErrConcurrentUpdate) || flaky.calls != 5 {
		t.Fatalf("err = %v after %d attempts: it must give up after 5", err, flaky.calls)
	}
}
