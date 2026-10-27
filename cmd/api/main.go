// Command api is product-discovery's composition root: it wires env config into
// adapters, adapters into use cases, and use cases into the HTTP router and the
// three Kafka consumers (ADR 0003), all on real Postgres and real Kafka: there
// is no in-memory mode and no consumer that is off. The service publishes
// nothing, so there is no outbox relay.
//
// Environment:
//
//	HTTP_ADDR                          listen address (default :8080)
//	DATABASE_URL                       Postgres DSN (REQUIRED)
//	MIGRATIONS_DATABASE_URL            direct DSN for the boot migrations (default DATABASE_URL)
//	KAFKA_BROKERS                      comma-separated brokers (REQUIRED: consumers, dead letters, /readyz)
//	OFFER_AVAILABILITY_CONSUMER_GROUP  stable group id of the consumer (REQUIRED)
//	PRODUCT_CONTENT_CONSUMER_GROUP     stable group id of the consumer (REQUIRED)
//	PRICING_CONSUMER_GROUP             stable group id of the consumer (REQUIRED)
//	PRICE_BAND_EDGES                   ascending minor-unit edges of the price facet (default 2500,5000,10000,25000)
//	SHUTDOWN_DRAIN_DELAY               wait after flipping /readyz before closing (default 5s)
//	LOG_LEVEL                          debug | info | warn | error (default info)
//	OTEL_SERVICE_NAME                  service.name of spans and metrics (default product-discovery)
//	OTEL_EXPORTER_OTLP_ENDPOINT        Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint); export never blocks
//	SERVICE_VERSION                    service.version (default dev)
//	ENVIRONMENT                        deployment.environment.name (default local)
//
// GET /readyz is 200 only while Postgres answers a ping AND a Kafka broker
// answers a metadata request (verdict cached 3s, bounded 2s); /healthz is
// liveness only. The process exits non-zero when a consumer returns on its own,
// so the orchestrator restarts it.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	outboundkafka "github.com/IQVO/product-discovery/internal/adapters/outbound/kafka"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

// workerDrainTimeout bounds how long shutdown waits for the consumers to return
// after their context is cancelled.
const workerDrainTimeout = 10 * time.Second

// DefaultShutdownDrainDelay is how long shutdown waits, after flipping /readyz
// to not-ready, before closing the listener.
const DefaultShutdownDrainDelay = 5 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

// run boots the service and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the integration test stops the real entry point that way).
func run(ctx context.Context) error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Misconfiguration is refused before anything is opened or bound.
	rt, err := loadRuntime(os.Getenv)
	if err != nil {
		return err
	}
	// Telemetry comes up before the router is built (otelchimetric binds the
	// global MeterProvider at construction) and is flushed LAST: deferred before
	// everything below, so the final spans of the drain still leave the process.
	tel := loadTelemetryEnv(os.Getenv)
	flushTelemetry, err := startTelemetry(logger, tel)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	// closeAdapters (pool.Close) is deferred FIRST so it runs LAST, after the
	// HTTP drain and the consumers have stopped.
	deps, pool, err := buildDeps(ctx, rt, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	readiness := newReadiness(pool.Ping, rt.brokers)
	httpServer := &http.Server{
		Addr:              getenv("HTTP_ADDR", ":8080"),
		Handler:           inboundhttp.NewRouter(&inboundhttp.Server{Queries: usecases.Queries{Deps: deps}, Readiness: readiness}, logger, tel.serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}
	consumers, closeConsumers := startConsumers(deps, rt, logger)
	return serveUntilSignal(ctx, logger, httpServer, readiness, consumers, closeConsumers)
}

// newReadiness builds the /readyz gate: the drain gate plus a Postgres ping and
// a Kafka broker answering a topic-less metadata request, cached 3s, bounded 2s.
// The ping is a parameter so the gate is testable without a pool.
func newReadiness(ping func(context.Context) error, brokers []string) *inboundhttp.Readiness {
	return inboundhttp.NewReadiness(inboundhttp.DefaultReadinessTTL, inboundhttp.DefaultReadinessTimeout,
		inboundhttp.Dependency{Name: "postgres", Check: ping},
		inboundhttp.Dependency{Name: "kafka", Check: outboundkafka.NewBrokerCheck(brokers).Check},
	)
}

// buildDeps wires the Postgres adapters, running the embedded migrations first.
func buildDeps(ctx context.Context, rt runtimeEnv, logger *slog.Logger) (usecases.Deps, *pgxpool.Pool, error) {
	// The first outbound dial of an injected pod can be reset (Istio native
	// sidecars), so migrations and the first ping retry with backoff; on
	// exhaustion the LAST error is returned and the process refuses to boot.
	migrationsURL := getenv("MIGRATIONS_DATABASE_URL", rt.databaseURL)
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(migrationsURL) }); err != nil {
		return usecases.Deps{}, nil, err
	}
	pool, err := postgres.NewPool(ctx, rt.databaseURL)
	if err != nil {
		return usecases.Deps{}, nil, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return usecases.Deps{}, nil, err
	}
	logger.Info("postgres adapters configured")
	return usecases.Deps{
		Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
		Listings: postgres.NewListingItemMap(pool), Inbox: postgres.NewInbox(pool), UoW: postgres.NewUnitOfWork(pool),
		Search: postgres.NewSearch(pool), Bands: rt.bandEdges, Log: logger,
	}, pool, nil
}
