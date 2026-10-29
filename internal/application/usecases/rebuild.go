package usecases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// PageSize is how many rows a rebuild asks each source for per page.
const PageSize = 500

// Sources are the snapshot reads of the rebuild (ADR 0003).
type Sources struct {
	Catalog  ports.CatalogSource
	Listings ports.ListingSource
	Prices   ports.PriceSource
	Contents ports.ContentSource
}

// Rebuilder is RebuildProjections: it seeds ProductView and CategoryView from
// the paged snapshot reads, through the same fold rules the events use, so the
// per-source guards of the snapshot rows drop every replayed event that is not
// newer. It never runs on a user request (cmd/rebuild).
type Rebuilder struct {
	Consumer
	Sources Sources
}

// Report counts the rows the rebuild folded per source.
type Report struct {
	Items, Listings, Prices, Contents, Categories int
	// Skipped counts the rows that could not be folded (shared.ErrInvalidFact):
	// they are logged and skipped, as the consumers dead-letter the same facts.
	Skipped int
}

// Run folds every source in turn: item master first, so most stubs are
// completed as they are created, then listings, prices, content and the tree.
// A failing page aborts the run; running it again is safe.
func (r Rebuilder) Run(ctx context.Context) (Report, error) {
	var rep Report
	steps := []struct {
		name string
		run  func(context.Context, *Report) error
	}{
		{"catalog", r.catalog}, {"listings", r.listings}, {"item prices", r.prices},
		{"product contents", r.contents}, {"categories", r.categories},
	}
	for _, s := range steps {
		if err := s.run(ctx, &rep); err != nil {
			return rep, fmt.Errorf("rebuild %s: %w", s.name, err)
		}
	}
	return rep, nil
}

// paged walks a keyset-paged source to its end.
func paged[T any](ctx context.Context, fetch func(after string) (readmodel.Page[T], error), each func(T) error) error {
	after := ""
	for {
		page, err := fetch(after)
		if err != nil {
			return err
		}
		for _, row := range page.Items {
			if err := each(row); err != nil {
				return err
			}
		}
		if page.Next == "" || ctx.Err() != nil {
			return ctx.Err()
		}
		after = page.Next
	}
}

// seed folds one snapshot row in its own unit of work.
func (r Rebuilder) seed(ctx context.Context, rep *Report, itemID string, rule func(productview.View) (productview.View, productview.Outcome, error)) error {
	return r.skipInvalid(ctx, rep, itemID, r.UoW.Do(ctx, func(ctx context.Context) error { return r.apply(ctx, itemID, rule) }))
}

// skipInvalid turns an unfoldable row into a logged skip; any other error
// aborts the run.
func (r Rebuilder) skipInvalid(ctx context.Context, rep *Report, key string, err error) error {
	if !errors.Is(err, shared.ErrInvalidFact) {
		return err
	}
	rep.Skipped++
	r.logger().WarnContext(ctx, "rebuild skipped a row that cannot be folded", "key", key, "error", err)
	return nil
}

func (r Rebuilder) catalog(ctx context.Context, rep *Report) error {
	return paged(ctx, func(after string) (readmodel.Page[readmodel.CatalogRow], error) {
		return r.Sources.Catalog.ListCatalog(ctx, after, PageSize)
	}, func(row readmodel.CatalogRow) error {
		rep.Items++
		return r.seed(ctx, rep, row.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			v, out, err := v.ApplyItemCreated(productview.ItemCreated{Title: row.Title, Hazmat: row.Hazmat, Fragile: row.Fragile})
			if err != nil || !row.Discontinued {
				return v, out, err
			}
			v, _ = v.ApplyItemDiscontinued()
			return v, productview.Applied, nil
		})
	})
}

func (r Rebuilder) prices(ctx context.Context, rep *Report) error {
	return paged(ctx, func(after string) (readmodel.Page[readmodel.PriceRow], error) {
		return r.Sources.Prices.ListItemPrices(ctx, after, PageSize)
	}, func(row readmodel.PriceRow) error {
		rep.Prices++
		return r.seed(ctx, rep, row.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			if row.Withdrawn {
				return v.ApplyPriceWithdrawn(row.Version)
			}
			return v.ApplyPriceChanged(productview.PriceChanged{
				Currency: row.Currency, ListPriceMinor: row.ListPriceMinor,
				EffectivePriceMinor: row.EffectivePriceMinor, Version: row.Version,
			})
		})
	})
}

func (r Rebuilder) contents(ctx context.Context, rep *Report) error {
	return paged(ctx, func(after string) (readmodel.Page[readmodel.ContentRow], error) {
		return r.Sources.Contents.ListProductContents(ctx, after, PageSize)
	}, func(row readmodel.ContentRow) error {
		rep.Contents++
		return r.seed(ctx, rep, row.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
			switch row.State {
			case "PUBLISHED":
				return v.ApplyContentPublished(row.Published)
			case "RETIRED":
				return v.ApplyContentRetired(row.Revision)
			}
			return v, productview.Stale, nil // a DRAFT never was published
		})
	})
}

func (r Rebuilder) categories(ctx context.Context, rep *Report) error {
	tree, err := r.Sources.Contents.ListCategories(ctx)
	if err != nil {
		return err
	}
	if tree.Version < 1 { // the producer has not published a tree yet
		return nil
	}
	rep.Categories = len(tree.Categories)
	return r.UoW.Do(ctx, func(ctx context.Context) error {
		return r.replaceTree(ctx, tree.Version, tree.Categories)
	})
}

func (r Rebuilder) listings(ctx context.Context, rep *Report) error {
	return paged(ctx, func(after string) (readmodel.Page[readmodel.ListingRow], error) {
		return r.Sources.Listings.ListListings(ctx, after, PageSize)
	}, func(row readmodel.ListingRow) error {
		rep.Listings++
		return r.skipInvalid(ctx, rep, row.ListingID, r.UoW.Do(ctx, func(ctx context.Context) error {
			if err := r.learn(ctx, row.ListingID, row.CatalogItemID); err != nil {
				return err
			}
			return r.apply(ctx, row.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
				return v.ApplyListingSnapshot(productview.ListingSnapshot{
					ListingID: row.ListingID, NodeID: row.NodeID, Suppressed: row.Suppressed, AvailableQty: row.AvailableQty, AsOf: row.AsOf,
				})
			})
		}))
	})
}

func (r Rebuilder) logger() *slog.Logger { return r.Deps.logger() }
