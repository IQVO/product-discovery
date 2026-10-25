//go:build integration

package postgres_test

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/repocontract"
	"github.com/IQVO/product-discovery/internal/testing/pgtest"
)

func TestPostgresHonoursTheRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(t *testing.T) repocontract.Env {
		pool := pgtest.NewPool(t)
		return repocontract.Env{
			Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
			Listings: postgres.NewListingItemMap(pool), Inbox: postgres.NewInbox(pool),
			UoW: postgres.NewUnitOfWork(pool), Search: postgres.NewSearch(pool),
		}
	})
}
