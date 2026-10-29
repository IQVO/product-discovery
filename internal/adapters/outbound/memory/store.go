// Package memory holds in-memory TEST DOUBLES of the application's out ports.
// They are for unit, BDD and contract tests only: cmd/** never imports this
// package (enforced by internal/architecture) because the service runs on real
// Postgres and Kafka.
//
// Every repository round-trips its row through Snapshot and Restore, so a
// loaded row never aliases stored state and a corrupt row can not become a
// live one; a miss is ports.ErrNotFound.
//
// One Store backs every double. Its UnitOfWork is atomic: it holds the store
// for the whole unit and restores the state taken at the start when the unit
// fails or panics, inbox included.
package memory

import (
	"context"
	"sync"

	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Store is the shared state of the in-memory doubles.
type Store struct {
	mu         sync.Mutex
	products   map[string]productview.View
	categories categoryview.View
	listings   map[string]string
	inbox      map[string]string
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{products: map[string]productview.View{}, listings: map[string]string{}, inbox: map[string]string{}}
}

// state is a copy of everything a unit of work can change. Stored values are
// replaced, never edited in place, so copying the maps is enough.
type state struct {
	products   map[string]productview.View
	categories categoryview.View
	listings   map[string]string
	inbox      map[string]string
}

func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) capture() state {
	return state{products: cloneMap(s.products), categories: s.categories, listings: cloneMap(s.listings), inbox: cloneMap(s.inbox)}
}

func (s *Store) restore(st state) {
	s.products, s.categories, s.listings, s.inbox = st.products, st.categories, st.listings, st.inbox
}

// txKey marks a ctx that carries the open unit of work of one store.
type txKey struct{ store *Store }

// enter locks the store for one repository call, unless ctx already carries the
// unit of work that holds it. The returned func releases the lock.
func (s *Store) enter(ctx context.Context) func() {
	if ctx.Value(txKey{s}) != nil {
		return func() {}
	}
	s.mu.Lock()
	return s.mu.Unlock
}

// Products returns the ProductRepository double.
func (s *Store) Products() *ProductRepository { return &ProductRepository{s} }

// Categories returns the CategoryRepository double.
func (s *Store) Categories() *CategoryRepository { return &CategoryRepository{s} }

// Listings returns the ListingItemMap double.
func (s *Store) Listings() *ListingItemMap { return &ListingItemMap{s} }

// Inbox returns the Inbox double.
func (s *Store) Inbox() *Inbox { return &Inbox{s} }

// UnitOfWork returns the UnitOfWork double.
func (s *Store) UnitOfWork() *UnitOfWork { return &UnitOfWork{s} }

// Search returns the SearchPort double.
func (s *Store) Search() *Search { return &Search{s} }

// Counts is how many rows each table of the store holds.
type Counts struct{ Products, Listings, Inbox int }

// Counts returns the size of every table, so a test can prove that a failed
// use case stored nothing.
func (s *Store) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Counts{Products: len(s.products), Listings: len(s.listings), Inbox: len(s.inbox)}
}
