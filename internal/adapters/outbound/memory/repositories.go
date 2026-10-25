package memory

import (
	"context"
	"fmt"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// ProductRepository is the in-memory ports.ProductRepository.
type ProductRepository struct{ s *Store }

// Get implements ports.ProductRepository.
func (r *ProductRepository) Get(ctx context.Context, id string) (productview.View, error) {
	defer r.s.enter(ctx)()
	v, ok := r.s.products[id]
	if !ok {
		return productview.View{}, fmt.Errorf("product %s: %w", id, ports.ErrNotFound)
	}
	return productview.Restore(v)
}

// GetForUpdate implements ports.ProductRepository; the unit of work already
// holds the store, which serialises every writer.
func (r *ProductRepository) GetForUpdate(ctx context.Context, id string) (productview.View, error) {
	return r.Get(ctx, id)
}

// Save implements ports.ProductRepository.
func (r *ProductRepository) Save(ctx context.Context, v productview.View) error {
	defer r.s.enter(ctx)()
	snap, err := productview.Restore(v.Snapshot())
	if err != nil {
		return err
	}
	r.s.products[v.CatalogItemID] = snap
	return nil
}

// PurchasableByCategory implements ports.ProductRepository.
func (r *ProductRepository) PurchasableByCategory(ctx context.Context) (map[string]int, error) {
	defer r.s.enter(ctx)()
	out := map[string]int{}
	for _, v := range r.s.products {
		if v.Purchasable() && v.CategoryID != "" {
			out[v.CategoryID]++
		}
	}
	return out, nil
}

// CategoryRepository is the in-memory ports.CategoryRepository.
type CategoryRepository struct{ s *Store }

// Current implements ports.CategoryRepository.
func (r *CategoryRepository) Current(ctx context.Context) (categoryview.View, error) {
	defer r.s.enter(ctx)()
	return r.s.categories, nil
}

// CurrentForUpdate implements ports.CategoryRepository.
func (r *CategoryRepository) CurrentForUpdate(ctx context.Context) (categoryview.View, error) {
	return r.Current(ctx)
}

// Replace implements ports.CategoryRepository.
func (r *CategoryRepository) Replace(ctx context.Context, v categoryview.View) error {
	defer r.s.enter(ctx)()
	r.s.categories = v
	return nil
}

// ListingItemMap is the in-memory ports.ListingItemMap.
type ListingItemMap struct{ s *Store }

// Get implements ports.ListingItemMap.
func (m *ListingItemMap) Get(ctx context.Context, listingID string) (string, error) {
	defer m.s.enter(ctx)()
	id, ok := m.s.listings[listingID]
	if !ok {
		return "", fmt.Errorf("listing %s: %w", listingID, ports.ErrNotFound)
	}
	return id, nil
}

// Put implements ports.ListingItemMap; a listing keeps its first item.
func (m *ListingItemMap) Put(ctx context.Context, listingID, catalogItemID string) error {
	defer m.s.enter(ctx)()
	if _, ok := m.s.listings[listingID]; !ok {
		m.s.listings[listingID] = catalogItemID
	}
	return nil
}

// Inbox is the in-memory ports.Inbox.
type Inbox struct{ s *Store }

// Claim implements ports.Inbox.
func (i *Inbox) Claim(ctx context.Context, eventID, eventType string) (bool, error) {
	defer i.s.enter(ctx)()
	if _, seen := i.s.inbox[eventID]; seen {
		return false, nil
	}
	i.s.inbox[eventID] = eventType
	return true, nil
}

// UnitOfWork is the in-memory ports.UnitOfWork.
type UnitOfWork struct{ s *Store }

// Do implements ports.UnitOfWork.
func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if ctx.Value(txKey{u.s}) != nil {
		return fn(ctx)
	}
	u.s.mu.Lock()
	defer u.s.mu.Unlock()
	before := u.s.capture()
	done := false
	defer func() {
		if !done || err != nil {
			u.s.restore(before)
		}
	}()
	err = fn(context.WithValue(ctx, txKey{u.s}, struct{}{}))
	done = true
	return err
}
