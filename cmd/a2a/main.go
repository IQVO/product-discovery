// Command a2a is product-discovery's Agent2Agent (A2A) composition root: the
// third deployable beside cmd/api (REST plus the three Kafka consumers) and
// cmd/rebuild (the ops job). It serves the READ-ONLY agent of ADR 0007 - two
// skills, search_products and get_product, over the same read use cases the
// REST adapter uses - on real Postgres. It consumes nothing, produces nothing
// and runs no scheduler: no Kafka, no outbox (this context publishes nothing),
// and no HTTP routes other than the agent.
//
// Environment:
//
//	A2A_ADDR                     listen address (default :8093)
//	A2A_AGENT_URL                absolute http(s) URL advertised on the Agent Card (REQUIRED)
//	DATABASE_URL                 Postgres DSN (REQUIRED)
//	MIGRATIONS_DATABASE_URL      direct DSN for the boot migrations (default DATABASE_URL)
//	PRICE_BAND_EDGES             ascending minor-unit edges of the price facet; keep equal to the api's (default 2500,5000,10000,25000)
//	LOG_LEVEL                    debug | info | warn | error (default info)
//	OTEL_SERVICE_NAME            service.name of spans and metrics (default product-discovery-a2a)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint); export never blocks
//	SERVICE_VERSION              service.version and the Agent Card version (default dev)
//	ENVIRONMENT                  deployment.environment.name (default local)
//
// The JSON-RPC endpoint answers at "/" and "/a2a", the Agent Card at
// /.well-known/agent-card.json and liveness at /healthz. The endpoints are
// unauthenticated by the fleet-wide decision (see the REST adapter).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

// run boots the agent and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the tests stop the real entry point that way).
func run(ctx context.Context) error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Misconfiguration is refused before anything is opened or bound.
	cfg, err := loadBootConfig(os.Getenv)
	if err != nil {
		return err
	}
	tel := loadTelemetryEnv(os.Getenv)
	flushTelemetry, err := startTelemetry(logger, tel)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	// pool.Close is deferred after the telemetry flush and before the server
	// starts, so it runs after the HTTP drain and the final spans still leave.
	queries, pool, err := buildQueries(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The business counter binds to the MeterProvider telemetry.Setup installed.
	searchMetrics, err := telemetry.NewSearchMetrics()
	if err != nil {
		return err
	}
	queries.Metrics = searchMetrics

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newAgent(queries, cfg.agentURL, tel.serviceVersion),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return serveUntilSignal(ctx, logger, srv)
}

// buildQueries wires the Postgres read side, running the embedded migrations
// first (idempotent: cmd/api runs the same ones). Only what the two read use
// cases need is built: the product and category repositories and the search
// port. There is no inbox, unit of work or listing map: the agent folds nothing.
func buildQueries(ctx context.Context, cfg bootConfig, logger *slog.Logger) (usecases.Queries, *pgxpool.Pool, error) {
	// The first outbound dial of an injected pod can be reset (Istio native
	// sidecars), so migrations and the first ping retry with backoff; on
	// exhaustion the LAST error is returned and the process refuses to boot.
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(cfg.migrationsURL) }); err != nil {
		return usecases.Queries{}, nil, err
	}
	pool, err := postgres.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return usecases.Queries{}, nil, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return usecases.Queries{}, nil, err
	}
	logger.Info("postgres read side configured")
	return usecases.Queries{Deps: usecases.Deps{
		Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
		Search: postgres.NewSearch(pool), Bands: cfg.bandEdges, Log: logger,
	}}, pool, nil
}

// serveUntilSignal runs srv until SIGINT/SIGTERM, ctx cancellation (or a listen
// error), then drains it within shutdownTimeout. The caller's deferred
// pool.Close runs after the drain.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("a2a agent listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		return err
	case <-sigCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
