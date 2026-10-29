package bdd

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cucumber/godog"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

// TestFeatures runs the Gherkin scenarios of ../../features against the use
// cases wired to the in-memory doubles, reading through the real HTTP router.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"../../features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

// world is the state of one scenario.
type world struct {
	store    *memory.Store
	deps     usecases.Deps
	consumer usecases.Consumer
	srv      *httptest.Server
	seq      int

	status int
	body   string
	json   map[string]any
	pages  []string
}

func newWorld() *world {
	s := memory.NewStore()
	d := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	w := &world{store: s, deps: d, consumer: usecases.Consumer{Deps: d}}
	w.srv = httptest.NewServer(inboundhttp.NewRouter(&inboundhttp.Server{Queries: usecases.Queries{Deps: d}}, nil, ""))
	return w
}

func (w *world) meta() usecases.Meta {
	w.seq++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", w.seq), Type: "test.Event", At: t0}
}

func initializeScenario(sc *godog.ScenarioContext) {
	w := newWorld()
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.srv.Close()
		*w = *newWorld()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.srv.Close()
		return ctx, nil
	})
	registerFacts(sc, w)
	registerSearch(sc, w)
	registerAssertions(sc, w)
	registerResults(sc, w)
	registerCategories(sc, w)
}
