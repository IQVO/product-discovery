package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/IQVO/product-discovery/internal/adapters/inbound/mcp"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newTestServer serves the real MCP server (no tool can reach a database: the
// zero Deps is enough for the transport tests) behind the real router.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newRouter(inboundmcp.Handler(inboundmcp.NewServer(inboundmcp.Deps{})), quietLogger(), "product-discovery-mcp"))
	t.Cleanup(srv.Close)
	return srv
}

func TestRouter_HealthzIsOpenAndServesOK(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Fatalf("GET /healthz = %d %q, want 200 {\"status\":\"ok\"}", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// Both mount points speak Streamable HTTP, with no credentials, and a real
// client lists the two read-only tools, the resource template and the prompt.
func TestStreamableHTTP_RootAndMCPPathsServeTheSurface(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/", "/mcp"} {
		client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
		session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL + path}, nil)
		if err != nil {
			t.Fatalf("connect %s: %v", path, err)
		}
		assertSurface(t, path, session)
		session.Close()
	}
}

// assertSurface checks the whole advertised surface of one connected session:
// two read-only L1 tools, one resource template and one prompt.
func assertSurface(t *testing.T, path string, session *sdk.ClientSession) {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools %s: %v", path, err)
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("%s: tools = %d, want 2", path, len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s: tool %q is not read-only", path, tool.Name)
		}
		if tool.Meta["com.warehouse/authority"] != "L1" {
			t.Fatalf("%s: tool %q authority = %v, want L1", path, tool.Name, tool.Meta["com.warehouse/authority"])
		}
	}
	if rts, err := session.ListResourceTemplates(context.Background(), nil); err != nil || len(rts.ResourceTemplates) != 1 {
		t.Fatalf("%s: resource templates = %v, %v, want 1", path, rts, err)
	}
	if ps, err := session.ListPrompts(context.Background(), nil); err != nil || len(ps.Prompts) != 1 {
		t.Fatalf("%s: prompts = %v, %v, want 1", path, ps, err)
	}
}

// The binary reads no API key: requests carrying (or lacking) any
// Authorization header behave identically.
func TestNoAuthIsRequired(t *testing.T) {
	srv := newTestServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	for _, auth := range []string{"", "Bearer nonsense"} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("initialize with Authorization=%q = %d, want 200", auth, resp.StatusCode)
		}
	}
}

// A missing DATABASE_URL is a boot error, never a silent in-memory fallback.
func TestBuildDeps_MissingDatabaseFailsBoot(t *testing.T) {
	_, _, err := buildReads(context.Background(), bootConfig{}, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("buildReads without a database = %v, want an error naming DATABASE_URL", err)
	}
}

// An unreachable database is a boot error after the bounded retries.
func TestBuildDeps_BadDatabaseFailsBoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a cancelled context makes bootretry give up at once
	dsn := "postgres://u@127.0.0.1:1/db"
	if _, _, err := buildReads(ctx, bootConfig{databaseURL: dsn, migrationsURL: dsn}, quietLogger()); err == nil {
		t.Fatal("an unreachable database must fail boot")
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("PD_MCP_TEST_SET", "v")
	t.Setenv("PD_MCP_TEST_EMPTY", "")
	if got := getenv("PD_MCP_TEST_SET", "d"); got != "v" {
		t.Errorf("set: %q", got)
	}
	if got := getenv("PD_MCP_TEST_EMPTY", "d"); got != "d" {
		t.Errorf("empty: %q", got)
	}
}

func TestNewLoggerLevels(t *testing.T) {
	for level, want := range map[string]slog.Level{"debug": slog.LevelDebug, "WARN": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo, "bogus": slog.LevelInfo} {
		if got := newLogger(level); !got.Handler().Enabled(context.Background(), want) || (want > slog.LevelDebug && got.Handler().Enabled(context.Background(), want-1)) {
			t.Errorf("newLogger(%q): level %v not the minimum enabled", level, want)
		}
	}
}

// ---- boot configuration -----------------------------------------------------

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadBootConfigDefaults(t *testing.T) {
	cfg, err := loadBootConfig(envOf(map[string]string{"DATABASE_URL": "postgres://db/app"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != ":8090" || cfg.databaseURL != "postgres://db/app" || cfg.migrationsURL != "postgres://db/app" || cfg.bandEdges != nil {
		t.Fatalf("defaults = %+v, want :8090, migrations DSN = DATABASE_URL, default band edges", cfg)
	}
}

func TestLoadBootConfigReadsTheEnvironment(t *testing.T) {
	cfg, err := loadBootConfig(envOf(map[string]string{
		"DATABASE_URL": "postgres://pool/app", "MIGRATIONS_DATABASE_URL": "postgres://direct/app",
		"MCP_ADDR": ":9999", "PRICE_BAND_EDGES": "100, 200,300",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != ":9999" || cfg.migrationsURL != "postgres://direct/app" || len(cfg.bandEdges) != 3 || cfg.bandEdges[2] != 300 {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadBootConfigRefusesBadValuesAllAtOnce(t *testing.T) {
	_, err := loadBootConfig(envOf(map[string]string{"PRICE_BAND_EDGES": "300,200"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") || !strings.Contains(err.Error(), "PRICE_BAND_EDGES") {
		t.Fatalf("err = %v, want both problems reported", err)
	}
	for _, edges := range []string{"abc", "0", "-5", "100,100"} {
		if _, err := loadBootConfig(envOf(map[string]string{"DATABASE_URL": "x", "PRICE_BAND_EDGES": edges})); err == nil {
			t.Errorf("PRICE_BAND_EDGES=%q was accepted", edges)
		}
	}
}
