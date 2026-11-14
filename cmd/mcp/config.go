package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
)

// errMissingDatabaseURL is the boot error of a missing DATABASE_URL: this
// binary has no in-memory mode. tests/render_test.sh scans these
// "<VAR> is required" strings to prove the chart renders every mandatory
// variable.
var errMissingDatabaseURL = errors.New("DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode")

// defaultAddr is the listen address: 8080 is the api, 8093 the A2A agent and
// 8090 the fleet's MCP port.
const defaultAddr = ":8090"

// bootConfig is the validated environment of the MCP server.
type bootConfig struct {
	databaseURL string
	// migrationsURL is the direct DSN for the boot migrations (default databaseURL).
	migrationsURL string
	addr          string
	// bandEdges are the price-band edges in minor units (nil = the default);
	// they must match the api's PRICE_BAND_EDGES so the facets agree.
	bandEdges []int64
}

// loadBootConfig validates the environment before anything is opened or bound
// and reports every problem at once, so an operator fixes it in one pass.
func loadBootConfig(env func(string) string) (bootConfig, error) {
	cfg := bootConfig{
		databaseURL:   env("DATABASE_URL"),
		migrationsURL: env("MIGRATIONS_DATABASE_URL"),
		addr:          env("MCP_ADDR"),
	}
	var errs []error
	if cfg.databaseURL == "" {
		errs = append(errs, errMissingDatabaseURL)
	}
	edges, err := parseBandEdges(env("PRICE_BAND_EDGES"))
	if err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return bootConfig{}, err
	}
	cfg.bandEdges = edges
	if cfg.migrationsURL == "" {
		cfg.migrationsURL = cfg.databaseURL
	}
	if cfg.addr == "" {
		cfg.addr = defaultAddr
	}
	return cfg, nil
}

// parseBandEdges reads PRICE_BAND_EDGES, ascending positive minor units
// ("2500,5000,10000,25000"); empty = nil (the default edges).
func parseBandEdges(raw string) ([]int64, error) {
	var edges []int64
	for _, b := range strings.Split(raw, ",") {
		s := strings.TrimSpace(b)
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 || (len(edges) > 0 && n <= edges[len(edges)-1]) {
			return nil, fmt.Errorf("PRICE_BAND_EDGES %q must be ascending positive integers (minor units)", raw)
		}
		edges = append(edges, n)
	}
	return edges, nil
}

// newLogger builds the JSON logger; LOG_LEVEL debug|info|warn|warning|error.
// The handler is wrapped so a record written with a span-carrying context (the
// *Context slog variants) also carries trace_id and span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
