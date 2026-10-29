package memory_test

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/repocontract"
)

func TestMemoryDoublesHonourTheRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(t *testing.T) repocontract.Env {
		s := memory.NewStore()
		return repocontract.Env{
			Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
			UoW: s.UnitOfWork(), Search: s.Search(),
		}
	})
}
