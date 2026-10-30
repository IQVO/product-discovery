package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testAgentURL = "http://product-discovery-a2a.warehouse-systems.svc.cluster.local:8093/"

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func fullEnv() map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:1/db?sslmode=disable", "A2A_AGENT_URL": testAgentURL}
}

func TestLoadBootConfigAcceptsACompleteEnvironmentAndDefaults(t *testing.T) {
	cfg, err := loadBootConfig(envOf(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	want := bootConfig{
		databaseURL: "postgres://u:p@127.0.0.1:1/db?sslmode=disable", migrationsURL: "postgres://u:p@127.0.0.1:1/db?sslmode=disable",
		agentURL: testAgentURL, addr: ":8093",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config = %+v, want %+v", cfg, want)
	}
}

func TestLoadBootConfigReadsTheOptionalSettings(t *testing.T) {
	env := fullEnv()
	env["A2A_ADDR"], env["MIGRATIONS_DATABASE_URL"], env["PRICE_BAND_EDGES"] = ":9999", "postgres://direct/db", "100, 200,300"
	cfg, err := loadBootConfig(envOf(env))
	if err != nil || cfg.addr != ":9999" || cfg.migrationsURL != "postgres://direct/db" || cfg.databaseURL == cfg.migrationsURL ||
		!reflect.DeepEqual(cfg.bandEdges, []int64{100, 200, 300}) {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
}

// Each missing or rejected value is a boot error that names its variable.
func TestLoadBootConfigRefusesEachMissingOrRejectedValue(t *testing.T) {
	cases := []struct {
		name, variable, value, want string
	}{
		{"database unset", "DATABASE_URL", "", "DATABASE_URL is required: this context runs on real Postgres"},
		{"agent url unset", "A2A_AGENT_URL", "", "A2A_AGENT_URL is required: the Agent Card must advertise"},
		{"agent url blank", "A2A_AGENT_URL", "   ", "A2A_AGENT_URL is required"},
		{"agent url relative", "A2A_AGENT_URL", "/a2a", `A2A_AGENT_URL="/a2a" is not an absolute http(s) URL`},
		{"agent url schemeless", "A2A_AGENT_URL", "product-discovery-a2a:8093", "is not an absolute http(s) URL"},
		{"agent url wrong scheme", "A2A_AGENT_URL", "ftp://host/", "is not an absolute http(s) URL"},
		{"agent url without host", "A2A_AGENT_URL", "http://", "is not an absolute http(s) URL"},
		{"band edges descending", "PRICE_BAND_EDGES", "200,100", "PRICE_BAND_EDGES"},
		{"band edges not numbers", "PRICE_BAND_EDGES", "x", "PRICE_BAND_EDGES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := fullEnv()
			env[tc.variable] = tc.value
			_, err := loadBootConfig(envOf(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s=%q: %v, want a boot error containing %q", tc.variable, tc.value, err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.variable) {
				t.Fatalf("the error does not name %s: %v", tc.variable, err)
			}
		})
	}
}

// Every problem is reported at once, so an operator fixes the environment in
// one pass.
func TestLoadBootConfigReportsEveryProblemAtOnce(t *testing.T) {
	_, err := loadBootConfig(envOf(nil))
	if err == nil || !errors.Is(err, errMissingDatabaseURL) || !errors.Is(err, errMissingAgentURL) {
		t.Fatalf("err = %v, want both variables named", err)
	}
}

// run fails fast on a bad configuration: before telemetry, any connection and
// before the listener binds.
func TestRunFailsFastBeforeBindingTheListener(t *testing.T) {
	for k, v := range fullEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("A2A_AGENT_URL", "not-a-url")
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("A2A_ADDR", addr)

	start := time.Now()
	if err := run(t.Context()); err == nil || !strings.Contains(err.Error(), "A2A_AGENT_URL") {
		t.Fatalf("run = %v, want a boot error naming A2A_AGENT_URL", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("a configuration error must not wait on any dependency")
	}
	if c, derr := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(t.Context(), "tcp", addr); derr == nil {
		_ = c.Close()
		t.Fatal("the listener was bound although the configuration is invalid")
	}
}

func TestRunRefusesToBootWithoutDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("A2A_AGENT_URL", testAgentURL)
	if err := run(t.Context()); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run() without DATABASE_URL = %v, want a boot error naming DATABASE_URL", err)
	}
}

func TestNewLoggerLevelsAndGetenv(t *testing.T) {
	for level, want := range map[string]slog.Level{"debug": slog.LevelDebug, "WARN": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError, "info": slog.LevelInfo, "": slog.LevelInfo, "loud": slog.LevelInfo} {
		l := newLogger(level)
		for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
			if got := l.Enabled(context.Background(), lvl); got != (lvl >= want) {
				t.Fatalf("level %q: enabled(%v) = %v", level, lvl, got)
			}
		}
	}
	if getenv("PRODUCT_DISCOVERY_A2A_UNSET", "fallback") != "fallback" {
		t.Fatal("getenv fallback")
	}
	t.Setenv("PRODUCT_DISCOVERY_A2A_SET", "v")
	if getenv("PRODUCT_DISCOVERY_A2A_SET", "fallback") != "v" {
		t.Fatal("getenv value")
	}
}

func TestLoadTelemetryEnvDefaultsToTheA2AServiceName(t *testing.T) {
	if got := loadTelemetryEnv(envOf(nil)); got.serviceName != "product-discovery-a2a" {
		t.Fatalf("default service name = %q, want product-discovery-a2a (distinct from the api)", got.serviceName)
	}
	if got := loadTelemetryEnv(envOf(map[string]string{"OTEL_SERVICE_NAME": "x"})); got.serviceName != "x" {
		t.Fatalf("OTEL_SERVICE_NAME ignored: %q", got.serviceName)
	}
}
