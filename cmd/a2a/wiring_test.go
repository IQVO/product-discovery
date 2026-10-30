package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestBuildQueriesUsesMigrationsURLNotDatabaseURLForMigrations proves
// buildQueries itself - not just the env read - threads the migrations DSN into
// the migration step and the application DSN into the pool, rather than the two
// ever being conflated (the application DSN may be a pooler that cannot run
// migrations). DATABASE_URL names an address nothing listens on and the
// migrations URL is a schemeless string migrate rejects immediately with a
// distinctive parse error.
//
// bootretry returns only ctx.Err() once its context ends (the cause of the
// attempts is in the log line it writes between attempts), so the context is
// cut after ~300ms - inside the first backoff - and the assertion reads the
// logged "retrying ... err=" line. A dial error there would mean the
// migrations ran against DATABASE_URL.
func TestBuildQueriesUsesMigrationsURLNotDatabaseURLForMigrations(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	cfg := bootConfig{
		databaseURL:   "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1",
		migrationsURL: "not-a-valid-connection-string",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	_, _, err := buildQueries(ctx, cfg, logger)
	if err == nil {
		t.Fatal("a malformed MIGRATIONS_DATABASE_URL must fail boot")
	}
	out := logs.String()
	if !strings.Contains(out, "run migrations") || !strings.Contains(out, "parse scheme") {
		t.Fatalf("log = %q - expected the parse error of the bogus migrations URL on the migrations step", out)
	}
	if strings.Contains(out, "connection refused") || strings.Contains(out, "ping postgres") {
		t.Fatalf("log = %q - the migrations must not reach the application DSN", out)
	}
}
