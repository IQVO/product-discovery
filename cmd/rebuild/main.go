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
//	LOG_LEVEL               debug | info | warn | error (default info)
//
// Optional telemetry (the job exports on exit; a missing Collector costs
// telemetry, never the run):
//
//	OTEL_SERVICE_NAME            service.name (default product-discovery-rebuild)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint)
//	SERVICE_VERSION              service.version (default dev)
//	ENVIRONMENT                  deployment.environment.name (default local)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/sources"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
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
	logger := newLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	flush := startTelemetry(ctx, logger, os.Getenv)
	defer flush()
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

// newLogger builds the JSON logger, wrapped so a record written with a
// span-carrying context also carries trace_id and span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

// telemetryFlushTimeout bounds the final export attempt of a finished job.
const telemetryFlushTimeout = 5 * time.Second

// startTelemetry brings OpenTelemetry up for the job and returns the flush to
// defer. A failed start or flush is logged, never fatal.
func startTelemetry(ctx context.Context, logger *slog.Logger, env func(string) string) func() {
	name := orDefault(env("OTEL_SERVICE_NAME"), "product-discovery-rebuild")
	endpoint := orDefault(env("OTEL_EXPORTER_OTLP_ENDPOINT"), telemetry.DefaultEndpoint)
	shutdown, err := telemetry.Setup(ctx, name, orDefault(env("SERVICE_VERSION"), telemetry.DefaultServiceVersion), endpoint)
	if err != nil {
		logger.Warn("telemetry disabled", "error", err)
		return func() {}
	}
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
		defer cancel()
		if err := shutdown(flushCtx); err != nil {
			logger.Warn("telemetry flush failed on shutdown", "error", err)
		}
	}
}

func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
