package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
)

// Boot errors for the variables the agent cannot run without. Each message
// names the variable; tests/render_test.sh scans these "<VAR> is required"
// strings to prove the chart renders every mandatory variable.
var (
	errMissingDatabaseURL = errors.New("DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode")
	errMissingAgentURL    = errors.New("A2A_AGENT_URL is required: the Agent Card must advertise an absolute http(s) URL clients can dial")
)

// defaultAddr is the listen address; 8080 is the api, 8093 the A2A agent.
const defaultAddr = ":8093"

// bootConfig is the validated environment of the agent.
type bootConfig struct {
	databaseURL string
	// migrationsURL is the direct DSN for the boot migrations (default databaseURL).
	migrationsURL string
	agentURL      string
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
		agentURL:      strings.TrimSpace(env("A2A_AGENT_URL")),
		addr:          env("A2A_ADDR"),
	}
	var errs []error
	if cfg.databaseURL == "" {
		errs = append(errs, errMissingDatabaseURL)
	}
	if err := checkAgentURL(cfg.agentURL); err != nil {
		errs = append(errs, err)
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

// checkAgentURL refuses an empty or non-absolute http(s) A2A_AGENT_URL: a card
// that advertises an undialable URL is worse than no card.
func checkAgentURL(raw string) error {
	if raw == "" {
		return errMissingAgentURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("A2A_AGENT_URL=%q is not an absolute http(s) URL (e.g. http://product-discovery-a2a.warehouse-systems.svc.cluster.local:8093/)", raw)
	}
	return nil
}

// splitList splits on commas, trimming blanks.
func splitList(raw string) []string {
	var out []string
	for _, b := range strings.Split(raw, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// parseBandEdges reads PRICE_BAND_EDGES, ascending positive minor units
// ("2500,5000,10000,25000"); empty = nil (the default edges).
func parseBandEdges(raw string) ([]int64, error) {
	var edges []int64
	for _, s := range splitList(raw) {
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
