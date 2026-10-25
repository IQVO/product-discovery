package usecases

import (
	"context"
	"fmt"

	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func invalidFact(format string, a ...any) error {
	return fmt.Errorf("%w: %s", shared.ErrInvalidFact, fmt.Sprintf(format, a...))
}

// ProductContentPublished is the consumed product-content event of the same
// name.
type ProductContentPublished struct {
	CatalogItemID string
	Content       productview.PublishedContent
}

// ProductContentRetired is the consumed product-content event of the same
// name; Revision is 0 when the producer sent none.
type ProductContentRetired struct {
	CatalogItemID string
	Revision      int64
}

// CategoryTreePublished is the consumed product-content event of the same name.
type CategoryTreePublished struct {
	Version    int64
	Categories []categoryview.Category
}

// ItemPriceChanged is the consumed pricing event of the same name.
type ItemPriceChanged struct {
	CatalogItemID string
	Price         productview.PriceChanged
}

// ItemPriceWithdrawn is the consumed pricing event of the same name.
type ItemPriceWithdrawn struct {
	CatalogItemID string
	Version       int64
}

// ApplyProductContentPublished replaces the content under the revision guard.
func (c Consumer) ApplyProductContentPublished(ctx context.Context, m Meta, in ProductContentPublished) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		return v.ApplyContentPublished(in.Content)
	})
}

// ApplyProductContentRetired ends the published content.
func (c Consumer) ApplyProductContentRetired(ctx context.Context, m Meta, in ProductContentRetired) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		return v.ApplyContentRetired(in.Revision)
	})
}

// ApplyItemPriceChanged sets the prices under the version guard.
func (c Consumer) ApplyItemPriceChanged(ctx context.Context, m Meta, in ItemPriceChanged) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		return v.ApplyPriceChanged(in.Price)
	})
}

// ApplyItemPriceWithdrawn withdraws the price.
func (c Consumer) ApplyItemPriceWithdrawn(ctx context.Context, m Meta, in ItemPriceWithdrawn) error {
	return c.fold(ctx, m, in.CatalogItemID, func(v productview.View) (productview.View, productview.Outcome, error) {
		return v.ApplyPriceWithdrawn(in.Version)
	})
}

// ApplyCategoryTreePublished replaces CategoryView under the version guard.
// Product rows keep only category ids, so nothing is re-indexed (H3).
func (c Consumer) ApplyCategoryTreePublished(ctx context.Context, m Meta, in CategoryTreePublished) error {
	return c.handle(ctx, m, func(ctx context.Context) error {
		return c.replaceTree(ctx, in.Version, in.Categories)
	})
}

func (c Consumer) replaceTree(ctx context.Context, version int64, cats []categoryview.Category) error {
	cur, err := c.Categories.CurrentForUpdate(ctx)
	if err != nil {
		return err
	}
	next, applied, err := categoryview.Replace(cur, version, cats)
	if err != nil || !applied {
		return err
	}
	return c.Categories.Replace(ctx, next)
}
