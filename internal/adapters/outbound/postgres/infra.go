package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-discovery/internal/application/ports"
)

// Inbox is the Postgres ports.Inbox over processed_events.
type Inbox struct{ pool *pgxpool.Pool }

var _ ports.Inbox = (*Inbox)(nil)

// NewInbox constructs an Inbox over pool.
func NewInbox(pool *pgxpool.Pool) *Inbox { return &Inbox{pool: pool} }

// Claim implements ports.Inbox. Called with a unit-of-work ctx the claim rolls
// back with the unit.
func (i *Inbox) Claim(ctx context.Context, eventID, eventType string) (bool, error) {
	first := false
	err := inTx(ctx, i.pool, func(q querier) error {
		tag, err := q.Exec(ctx, `INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2) ON CONFLICT (event_id) DO NOTHING`, eventID, eventType)
		first = err == nil && tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claim event %s: %w", eventID, err)
	}
	return first, nil
}

// ListingItemMap is the Postgres ports.ListingItemMap over listing_items.
type ListingItemMap struct{ pool *pgxpool.Pool }

var _ ports.ListingItemMap = (*ListingItemMap)(nil)

// NewListingItemMap constructs a ListingItemMap over pool.
func NewListingItemMap(pool *pgxpool.Pool) *ListingItemMap { return &ListingItemMap{pool: pool} }

// Get implements ports.ListingItemMap.
func (m *ListingItemMap) Get(ctx context.Context, listingID string) (string, error) {
	var id string
	err := queryFor(ctx, m.pool).QueryRow(ctx, `SELECT catalog_item_id FROM listing_items WHERE listing_id = $1`, listingID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("listing %s: %w", listingID, ports.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("read listing %s: %w", listingID, err)
	}
	return id, nil
}

// Put implements ports.ListingItemMap; a listing keeps its first item.
func (m *ListingItemMap) Put(ctx context.Context, listingID, catalogItemID string) error {
	_, err := queryFor(ctx, m.pool).Exec(ctx, `INSERT INTO listing_items (listing_id, catalog_item_id) VALUES ($1, $2) ON CONFLICT (listing_id) DO NOTHING`, listingID, catalogItemID)
	if err != nil {
		return fmt.Errorf("store listing %s: %w", listingID, err)
	}
	return nil
}
