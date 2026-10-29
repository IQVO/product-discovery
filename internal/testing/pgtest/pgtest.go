// Package pgtest boots ONE real Postgres through testcontainers per test
// binary and hands every test its own database, cloned from a template that
// already has the embedded migrations applied. It never reads DATABASE_URL and
// never skips: a missing Docker is a test failure (fleet rule,
// TestPostgresIntegrationTestsUseTestcontainers).
package pgtest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"
)

const (
	user         = "discovery_test"
	password     = "discovery_test"
	adminDB      = "postgres"
	templateName = "discovery_template"
)

var (
	once      sync.Once
	container *tcpostgres.PostgresContainer
	startErr  error
	seq       atomic.Int64
)

func start() {
	ctx := context.Background()
	container, startErr = tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase(adminDB),
		tcpostgres.WithUsername(user),
		tcpostgres.WithPassword(password),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if startErr != nil {
		return
	}
	startErr = buildTemplate(ctx)
}

// buildTemplate creates the template database and applies the migrations to it
// (twice: boot runs them on every start, the second run must be a no-op).
func buildTemplate(ctx context.Context) error {
	if err := adminExec(ctx, `CREATE DATABASE `+templateName); err != nil {
		return err
	}
	url, err := databaseURL(ctx, templateName)
	if err != nil {
		return err
	}
	for range 2 {
		if err := postgres.RunMigrations(url); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}
	return nil
}

func databaseURL(ctx context.Context, db string) (string, error) {
	base, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", err
	}
	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, password, cfg.Host, cfg.Port, db), nil
}

func adminExec(ctx context.Context, sql string) error {
	url, err := databaseURL(ctx, adminDB)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// URL returns the DSN of a FRESH database (migrations applied) on the shared
// container.
func URL(t *testing.T) string {
	t.Helper()
	once.Do(start)
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("t_%d", seq.Add(1))
	if err := adminExec(ctx, `CREATE DATABASE `+name+` TEMPLATE `+templateName); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	url, err := databaseURL(ctx, name)
	if err != nil {
		t.Fatalf("database url: %v", err)
	}
	return url
}

// EmptyURL returns the DSN of a fresh database WITHOUT migrations (for the
// up/down/up proof).
func EmptyURL(t *testing.T) string {
	t.Helper()
	once.Do(start)
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("e_%d", seq.Add(1))
	if err := adminExec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	url, err := databaseURL(ctx, name)
	if err != nil {
		t.Fatalf("database url: %v", err)
	}
	return url
}

// NewPool returns a pool on a fresh database (migrations applied), closed when
// the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), URL(t))
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// EventProcessed reports whether the consumer inbox of the database at dsn has
// claimed the CloudEvents id (a consumed event was applied or skipped).
func EventProcessed(t *testing.T, dsn, eventID string) bool {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM processed_events WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		t.Fatalf("read the inbox: %v", err)
	}
	return n > 0
}

// Shutdown terminates the shared container; call it from TestMain.
func Shutdown() {
	if container != nil {
		_ = testcontainers.TerminateContainer(container)
	}
}
