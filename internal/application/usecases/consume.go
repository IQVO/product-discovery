package usecases

import (
	"context"
	"errors"
	"time"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Meta is what the Kafka adapter hands over about the CloudEvents envelope.
type Meta struct {
	EventID string
	Type    string
	At      time.Time
}

// Consumer applies the events product-discovery consumes (ADR 0003). Every
// method runs in one unit of work together with the inbox claim, so an event
// is applied at most once.
type Consumer struct{ Deps }

// consumerAttempts is how often a fold runs when another writer got in
// between its read and its commit (a serialization failure).
const consumerAttempts = 5

// handle claims the event and runs apply in the same unit of work.
func (c Consumer) handle(ctx context.Context, m Meta, apply func(ctx context.Context) error) error {
	var err error
	for i := 0; i < consumerAttempts; i++ {
		err = c.UoW.Do(ctx, func(ctx context.Context) error {
			first, err := c.Inbox.Claim(ctx, m.EventID, m.Type)
			if err != nil || !first {
				return err
			}
			return apply(ctx)
		})
		if !errors.Is(err, ports.ErrConcurrentUpdate) {
			break
		}
	}
	return err
}

// fold loads the row of itemID (a fresh stub when none), applies one domain
// fold rule and saves the row when the rule applied.
func (c Consumer) fold(ctx context.Context, m Meta, itemID string, rule func(productview.View) (productview.View, productview.Outcome, error)) error {
	return c.handle(ctx, m, func(ctx context.Context) error {
		return c.apply(ctx, itemID, rule)
	})
}

// apply is the body of fold, also used by the rebuild (which has no inbox).
func (c Consumer) apply(ctx context.Context, itemID string, rule func(productview.View) (productview.View, productview.Outcome, error)) error {
	fresh, err := productview.Stub(itemID)
	if err != nil {
		return err
	}
	v, err := c.Products.GetForUpdate(ctx, itemID)
	if errors.Is(err, ports.ErrNotFound) {
		v, err = fresh, nil
	}
	if err != nil {
		return err
	}
	next, out, err := rule(v)
	if err != nil || out == productview.Stale {
		return err
	}
	return c.Products.Save(ctx, next)
}
