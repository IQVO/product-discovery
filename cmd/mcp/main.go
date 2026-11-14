// Command mcp is the composition root for the product-discovery MCP server
// (docs/adr/0009-read-only-mcp-server.md): it wires env config to the outbound
// Postgres read repositories, the repositories to the SAME read use cases the
// REST adapter and the A2A agent expose (SearchProducts, GetProduct), and those
// to the inbound MCP adapter, then serves MCP over Streamable HTTP (and nothing
// else: no stdio, no SSE). It is a separate deployable beside cmd/api, cmd/a2a
// and cmd/rebuild, shipped in the same image as /app/mcp.
//
// The surface is READ-ONLY: no write use case and no write seam is wired here
// at all (no unit of work, no inbox, no listing map), so this binary can never
// fold a fact. It consumes nothing, publishes nothing and never dials Kafka
// (no KAFKA_BROKERS, no consumer groups): the three consumers run in cmd/api
// only. It makes no REST, A2A or MCP call to any sibling context.
//
// There is NO auth of any kind (fleet-wide revert 2026-09-11): no keys, no
// bearer checks. Access control is the in-cluster ClusterIP boundary.
//
// Environment:
//
//	MCP_ADDR                     listen address (default :8090)
//	DATABASE_URL                 REQUIRED. Postgres DSN (the service database cmd/api owns)
//	MIGRATIONS_DATABASE_URL      direct DSN for the boot migrations (default DATABASE_URL)
//	PRICE_BAND_EDGES             ascending minor-unit edges of the price facet; keep equal to the api's (default 2500,5000,10000,25000)
//	LOG_LEVEL                    debug | info | warn | error (default info)
//	OTEL_SERVICE_NAME            service.name of spans and metrics (default product-discovery-mcp)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint); export never blocks
//	SERVICE_VERSION              service.version (default dev)
//	ENVIRONMENT                  deployment.environment.name (default local)
//
// The MCP endpoint answers at "/" and "/mcp" and liveness at /healthz.
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

	inboundmcp "github.com/IQVO/product-discovery/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}

// run boots the server and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the integration test stops the real entry point that way).
func run(ctx context.Context) error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Misconfiguration is refused before anything is opened or bound.
	cfg, err := loadBootConfig(os.Getenv)
	if err != nil {
		return err
	}

	// Telemetry comes up before the router is built (otelchimetric binds the
	// global MeterProvider at construction) and is flushed last.
	tel := loadTelemetryEnv(os.Getenv)
	flushTelemetry, err := startTelemetry(logger, tel)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	reads, closeAdapters, err := buildReads(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()

	// The business counter binds to the MeterProvider telemetry.Setup installed.
	searchMetrics, err := telemetry.NewSearchMetrics()
	if err != nil {
		return err
	}
	reads.Metrics = searchMetrics

	logger.Info("mcp server starting", "version", tel.serviceVersion, "environment", getenv("ENVIRONMENT", "local"))
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newRouter(inboundmcp.Handler(inboundmcp.NewServer(inboundmcp.Deps{Reads: reads})), logger, tel.serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return serveUntilSignal(ctx, logger, srv)
}

// buildReads migrates the database, connects a pgx pool and wires the read use
// cases over the Postgres repositories: the same product and category
// repositories and search port the REST adapter serves its GETs from. The
// returned close func releases the pool. An empty DATABASE_URL is a boot error:
// there is no fallback runtime.
//
// Like cmd/api this binary runs the idempotent embedded migrations on start, so
// it can boot against a fresh database before cmd/api has; golang-migrate's
// advisory lock makes concurrent starts safe. migrationsURL is used ONLY for
// that step (a DIRECT Postgres DSN where DATABASE_URL points at a pooled
// connection, which cannot honour the session-scoped advisory lock); the
// runtime pool always uses databaseURL.
func buildReads(ctx context.Context, cfg bootConfig, logger *slog.Logger) (usecases.Queries, func(), error) {
	noop := func() {}
	if cfg.databaseURL == "" {
		return usecases.Queries{}, noop, errMissingDatabaseURL
	}
	// The first outbound dial of an injected pod can be reset (native
	// sidecars), so migrations and the first ping retry with backoff; on
	// exhaustion the LAST error is returned and the process refuses to boot.
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(cfg.migrationsURL) }); err != nil {
		return usecases.Queries{}, noop, err
	}
	pool, err := postgres.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return usecases.Queries{}, noop, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return usecases.Queries{}, noop, err
	}
	logger.Info("postgres read side configured")
	// READ-ONLY wiring: only what the two read use cases touch. No inbox, unit
	// of work or listing map is constructed here, so this binary has no seam to
	// fold through at all.
	return usecases.Queries{Deps: usecases.Deps{
		Products: postgres.NewProductRepository(pool), Categories: postgres.NewCategoryRepository(pool),
		Search: postgres.NewSearch(pool), Bands: cfg.bandEdges, Log: logger,
	}}, pool.Close, nil
}

// serveUntilSignal runs srv until SIGINT/SIGTERM, ctx cancellation (or a listen
// error), then drains it within shutdownTimeout. The caller's deferred close of
// the pool runs after the drain.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("mcp server listening (Streamable HTTP)", "addr", srv.Addr)
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
