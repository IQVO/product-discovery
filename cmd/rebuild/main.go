// Command rebuild is RebuildProjections (ADR 0003, gap S5): an ops job that
// seeds ProductView and CategoryView from the paged snapshot reads of
// offer-availability (GET /catalog, and the ops-only GET /listings), pricing
// (GET /item-prices) and product-content (GET /product-contents, GET
// /categories). It never runs on a user request. The Kafka consumers of
// cmd/api resume from their committed offsets afterwards; the per-source
// guards seeded by the snapshot drop every replayed event that is not newer.
//
// Environment (all REQUIRED, the error names the variable):
//
//	DATABASE_URL            Postgres DSN
//	OFFER_AVAILABILITY_URL  base URL of offer-availability
//	PRICING_URL             base URL of pricing
//	PRODUCT_CONTENT_URL     base URL of product-content
//	LOG_LEVEL
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/sources"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/bootretry"
)

// config is the validated environment of the job.
type config struct {
	databaseURL, offerAvailabilityURL, pricingURL, productContentURL string
}

var requiredEnv = []string{"DATABASE_URL", "OFFER_AVAILABILITY_URL", "PRICING_URL", "PRODUCT_CONTENT_URL"}

func loadConfig(env func(string) string) (config, error) {
	for _, name := range requiredEnv {
		if strings.TrimSpace(env(name)) == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
	}
	return config{
		databaseURL: env("DATABASE_URL"), offerAvailabilityURL: env("OFFER_AVAILABILITY_URL"),
		pricingURL: env("PRICING_URL"), productContentURL: env("PRODUCT_CONTENT_URL"),
	}, nil
}

func main() { os.Exit(exitCode()) }

func exitCode() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(ctx, os.Getenv, logger); err != nil {
		logger.Error("rebuild failed", "error", err)
		return 1
	}
	return 0
}

// run migrates, wires the sources and folds every snapshot page.
func run(ctx context.Context, env func(string) string, logger *slog.Logger) error {
	cfg, err := loadConfig(env)
	if err != nil {
		return err
	}
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(cfg.databaseURL) }); err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	deps := usecases.Deps{
		Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
		Listings: postgres.NewListingItemMap(pool), Inbox: postgres.NewInbox(pool), UoW: postgres.NewUnitOfWork(pool), Log: logger,
	}
	oa := sources.OfferAvailability{Client: sources.Client{BaseURL: cfg.offerAvailabilityURL}}
	content := sources.ProductContent{Client: sources.Client{BaseURL: cfg.productContentURL}}
	pricing := sources.Pricing{Client: sources.Client{BaseURL: cfg.pricingURL}}
	rep, err := usecases.Rebuilder{
		Consumer: usecases.Consumer{Deps: deps},
		Sources:  usecases.Sources{Catalog: oa, Listings: oa, Prices: pricing, Contents: content},
	}.Run(ctx)
	logger.Info("rebuild finished", "items", rep.Items, "listings", rep.Listings, "prices", rep.Prices,
		"contents", rep.Contents, "categories", rep.Categories)
	return err
}
