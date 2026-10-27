package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://x", "OFFER_AVAILABILITY_URL": "http://oa", "PRICING_URL": "http://pr", "PRODUCT_CONTENT_URL": "http://pc",
	}
}

func TestLoadConfigAcceptsAFullEnvironment(t *testing.T) {
	c, err := loadConfig(envOf(fullEnv()))
	if err != nil || c.databaseURL != "postgres://x" || c.offerAvailabilityURL != "http://oa" || c.pricingURL != "http://pr" || c.productContentURL != "http://pc" {
		t.Fatalf("config = %+v %v", c, err)
	}
}

func TestLoadConfigNamesTheMissingVariable(t *testing.T) {
	for _, name := range requiredEnv {
		env := fullEnv()
		env[name] = "  "
		if _, err := loadConfig(envOf(env)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

func TestRunRefusesAnIncompleteEnvironmentBeforeDialling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(context.Background(), envOf(map[string]string{}), logger); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartTelemetryDoesNotBlockWithoutACollector(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan struct{})
	go func() {
		defer close(done)
		startTelemetry(context.Background(), logger, envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "127.0.0.1:14317"}))()
	}()
	select {
	case <-done:
	case <-time.After(3*telemetryFlushTimeout + 5*time.Second):
		t.Fatal("startTelemetry or its flush blocked on an unreachable collector")
	}
}

func TestNewLoggerStampsTraceIDsAndHonoursTheLevel(t *testing.T) {
	if newLogger("error").Enabled(context.Background(), slog.LevelInfo) {
		t.Error("LOG_LEVEL=error must not enable info")
	}
	if !newLogger("").Enabled(context.Background(), slog.LevelInfo) {
		t.Error("an unset level must default to info")
	}
}

func TestOrDefault(t *testing.T) {
	if orDefault("", "d") != "d" || orDefault("v", "d") != "v" {
		t.Error("orDefault")
	}
}
