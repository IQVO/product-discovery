package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// ProductRepository is the Postgres ports.ProductRepository over products.
type ProductRepository struct{ pool *pgxpool.Pool }

var _ ports.ProductRepository = (*ProductRepository)(nil)

// NewProductRepository constructs a ProductRepository over pool.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

type attributeJSON struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type mediaJSON struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

type listingJSON struct {
	ListingID    string    `json:"listingId"`
	NodeID       string    `json:"nodeId"`
	State        string    `json:"state"`
	AvailableQty int64     `json:"availableQty"`
	AsOf         time.Time `json:"asOf"`
}

const selectColumns = `catalog_item_id, title, hazmat, fragile, item_state, content_state, content_revision, description,
	bullets, attributes, media, category_id, price_state, price_version, currency, list_price_minor, effective_price_minor, listings`

// Get implements ports.ProductRepository.
func (r *ProductRepository) Get(ctx context.Context, id string) (productview.View, error) {
	return r.read(ctx, id)
}

// GetForUpdate implements ports.ProductRepository: inside a unit of work it
// takes a transaction-scoped advisory lock on the item first, so concurrent
// folds of one item (three consumer groups, the rebuild) run one after the
// other. The lock works for an item that has no row yet.
func (r *ProductRepository) GetForUpdate(ctx context.Context, id string) (productview.View, error) {
	if inUnitOfWork(ctx) {
		if _, err := queryFor(ctx, r.pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, id); err != nil {
			return productview.View{}, fmt.Errorf("lock product %s: %w", id, err)
		}
	}
	return r.read(ctx, id)
}

func (r *ProductRepository) read(ctx context.Context, id string) (productview.View, error) {
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+selectColumns+` FROM products WHERE catalog_item_id = $1`, id)
	v, err := scanView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return productview.View{}, fmt.Errorf("product %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return productview.View{}, fmt.Errorf("read product %s: %w", id, err)
	}
	return v, nil
}

func scanView(row pgx.Row) (productview.View, error) {
	var v productview.View
	var bullets, attrs, media, listings []byte
	var item, content, price string
	if err := row.Scan(&v.CatalogItemID, &v.Title, &v.Hazmat, &v.Fragile, &item, &content, &v.ContentRevision, &v.Description,
		&bullets, &attrs, &media, &v.CategoryID, &price, &v.PriceVersion, &v.Currency, &v.ListPriceMinor, &v.EffectivePriceMinor, &listings); err != nil {
		return productview.View{}, err
	}
	v.Item, v.Content, v.Price = productview.ItemState(item), productview.ContentState(content), productview.PriceState(price)
	if err := decodeColumns(&v, bullets, attrs, media, listings); err != nil {
		return productview.View{}, err
	}
	return productview.Restore(v)
}

func decodeColumns(v *productview.View, bullets, attrs, media, listings []byte) error {
	var a []attributeJSON
	var m []mediaJSON
	var l []listingJSON
	for _, d := range []struct {
		raw []byte
		to  any
	}{{bullets, &v.Bullets}, {attrs, &a}, {media, &m}, {listings, &l}} {
		if err := json.Unmarshal(d.raw, d.to); err != nil {
			return fmt.Errorf("decode stored product %s: %w", v.CatalogItemID, err)
		}
	}
	for _, x := range a {
		v.Attributes = append(v.Attributes, productview.Attribute(x))
	}
	for _, x := range m {
		v.Media = append(v.Media, productview.Media(x))
	}
	for _, x := range l {
		v.Listings = append(v.Listings, productview.Listing{
			ListingID: x.ListingID, NodeID: x.NodeID, State: productview.ListingState(x.State), AvailableQty: x.AvailableQty, AsOf: x.AsOf,
		})
	}
	return nil
}

func attributeText(v productview.View) string {
	var parts []string
	for _, a := range v.Attributes {
		parts = append(parts, a.Name, a.Value)
	}
	return strings.Join(parts, " ")
}
