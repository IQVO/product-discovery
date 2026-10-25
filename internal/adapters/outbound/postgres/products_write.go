package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Save implements ports.ProductRepository: it upserts the row together with
// the derived flags and the search document.
func (r *ProductRepository) Save(ctx context.Context, v productview.View) error {
	attrs, media, listings := make([]attributeJSON, 0, len(v.Attributes)), make([]mediaJSON, 0, len(v.Media)), make([]listingJSON, 0, len(v.Listings))
	for _, a := range v.Attributes {
		attrs = append(attrs, attributeJSON(a))
	}
	for _, m := range v.Media {
		media = append(media, mediaJSON(m))
	}
	for _, l := range v.Listings {
		listings = append(listings, listingJSON{ListingID: l.ListingID, NodeID: l.NodeID, State: string(l.State), AvailableQty: l.AvailableQty, AsOf: l.AsOf, StateKnown: l.StateKnown})
	}
	bullets := v.Bullets
	if bullets == nil {
		bullets = []string{}
	}
	cols, err := marshalAll(bullets, attrs, media, listings)
	if err != nil {
		return err
	}
	_, err = queryFor(ctx, r.pool).Exec(ctx, upsertSQL,
		v.CatalogItemID, v.Title, strings.ToLower(v.Title), v.Hazmat, v.Fragile, string(v.Item), string(v.Content), v.ContentRevision,
		v.Description, cols[0], cols[1], cols[2], v.CategoryID, string(v.Price), v.PriceVersion, v.Currency, v.ListPriceMinor,
		v.EffectivePriceMinor, cols[3], v.InStock(), v.Purchasable(), v.Searchable(), v.HasPrice(),
		v.Title, attributeText(v), v.Description+" "+strings.Join(v.Bullets, " "))
	if err != nil {
		return fmt.Errorf("save product %s: %w", v.CatalogItemID, err)
	}
	return nil
}

func marshalAll(vals ...any) ([][]byte, error) {
	out := make([][]byte, len(vals))
	for i, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode product column: %w", err)
		}
		out[i] = b
	}
	return out, nil
}

const upsertSQL = `
INSERT INTO products (catalog_item_id, title, title_sort, hazmat, fragile, item_state, content_state, content_revision, description,
	bullets, attributes, media, category_id, price_state, price_version, currency, list_price_minor, effective_price_minor, listings,
	in_stock, purchasable, searchable, has_price, doc)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
	setweight(to_tsvector('english', $24), 'A') || setweight(to_tsvector('english', $25), 'B') || setweight(to_tsvector('english', $26), 'C'))
ON CONFLICT (catalog_item_id) DO UPDATE SET
	title = EXCLUDED.title, title_sort = EXCLUDED.title_sort, hazmat = EXCLUDED.hazmat, fragile = EXCLUDED.fragile,
	item_state = EXCLUDED.item_state, content_state = EXCLUDED.content_state, content_revision = EXCLUDED.content_revision,
	description = EXCLUDED.description, bullets = EXCLUDED.bullets, attributes = EXCLUDED.attributes, media = EXCLUDED.media,
	category_id = EXCLUDED.category_id, price_state = EXCLUDED.price_state, price_version = EXCLUDED.price_version,
	currency = EXCLUDED.currency, list_price_minor = EXCLUDED.list_price_minor, effective_price_minor = EXCLUDED.effective_price_minor,
	listings = EXCLUDED.listings, in_stock = EXCLUDED.in_stock, purchasable = EXCLUDED.purchasable,
	searchable = EXCLUDED.searchable, has_price = EXCLUDED.has_price, doc = EXCLUDED.doc`

// PurchasableByCategory implements ports.ProductRepository.
func (r *ProductRepository) PurchasableByCategory(ctx context.Context) (map[string]int, error) {
	rows, err := queryFor(ctx, r.pool).Query(ctx, `SELECT category_id, count(*) FROM products WHERE purchasable AND category_id <> '' GROUP BY category_id`)
	if err != nil {
		return nil, fmt.Errorf("count purchasable products: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
