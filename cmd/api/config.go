package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/IQVO/product-discovery/internal/adapters/outbound/telemetry"
)

// Boot errors for the variables the context cannot run without. Each message
// names the variable.
var (
	errMissingDatabaseURL = errors.New("DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode")
	errMissingBrokers     = errors.New("KAFKA_BROKERS is required: this context runs on real Kafka (consumers, dead letters, /readyz)")
)

// The consumer group variables; every consumer always runs.
var consumerGroupEnvs = []string{
	"OFFER_AVAILABILITY_CONSUMER_GROUP", "PRODUCT_CONTENT_CONSUMER_GROUP", "PRICING_CONSUMER_GROUP",
}

// runtimeEnv is the validated part of the environment that selects the real
// runtime.
type runtimeEnv struct {
	databaseURL string
	brokers     []string
	// groups maps a consumer's group variable to its value.
	groups map[string]string
	// bandEdges are the price-band edges in minor units (nil = the default).
	bandEdges []int64
}

// loadRuntime validates the environment before anything is opened or bound:
// DATABASE_URL, KAFKA_BROKERS and every consumer group are mandatory. Every
// error names the variable.
func loadRuntime(env func(string) string) (runtimeEnv, error) {
	rt := runtimeEnv{databaseURL: env("DATABASE_URL"), groups: map[string]string{}}
	if rt.databaseURL == "" {
		return runtimeEnv{}, errMissingDatabaseURL
	}
	if rt.brokers = splitList(env("KAFKA_BROKERS")); len(rt.brokers) == 0 {
		return runtimeEnv{}, errMissingBrokers
	}
	for _, name := range consumerGroupEnvs {
		group := env(name)
		if group == "" {
			return runtimeEnv{}, fmt.Errorf("%s is required: the consumer always runs on real Kafka", name)
		}
		rt.groups[name] = group
	}
	edges, err := parseBandEdges(env("PRICE_BAND_EDGES"))
	if err != nil {
		return runtimeEnv{}, err
	}
	rt.bandEdges = edges
	return rt, nil
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

// shutdownDrainDelay reads SHUTDOWN_DRAIN_DELAY ("0" disables; unset, negative
// or unparsable = the default).
func shutdownDrainDelay(logger *slog.Logger) time.Duration {
	raw := os.Getenv("SHUTDOWN_DRAIN_DELAY")
	if raw == "" {
		return DefaultShutdownDrainDelay
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		logger.Warn("ignoring invalid SHUTDOWN_DRAIN_DELAY", "value", raw, "default", DefaultShutdownDrainDelay.String())
		return DefaultShutdownDrainDelay
	}
	return d
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
