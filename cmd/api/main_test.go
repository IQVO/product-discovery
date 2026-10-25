package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://x", "KAFKA_BROKERS": "a:9092, b:9092",
		"OFFER_AVAILABILITY_CONSUMER_GROUP": "g1", "PRODUCT_CONTENT_CONSUMER_GROUP": "g2", "PRICING_CONSUMER_GROUP": "g3",
	}
}

func TestLoadRuntimeAcceptsAFullEnvironment(t *testing.T) {
	rt, err := loadRuntime(envOf(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if rt.databaseURL != "postgres://x" || len(rt.brokers) != 2 || rt.brokers[1] != "b:9092" || len(rt.groups) != 3 ||
		rt.groups["PRICING_CONSUMER_GROUP"] != "g3" || rt.bandEdges != nil {
		t.Fatalf("runtime = %+v", rt)
	}
}

func TestLoadRuntimeNamesTheMissingVariable(t *testing.T) {
	for _, name := range append([]string{"DATABASE_URL", "KAFKA_BROKERS"}, consumerGroupEnvs...) {
		env := fullEnv()
		delete(env, name)
		_, err := loadRuntime(envOf(env))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("without %s: %v", name, err)
		}
	}
	env := fullEnv()
	env["KAFKA_BROKERS"] = " , "
	if _, err := loadRuntime(envOf(env)); !errors.Is(err, errMissingBrokers) {
		t.Fatalf("blank brokers: %v", err)
	}
}

func TestBandEdgesAreConfigurable(t *testing.T) {
	env := fullEnv()
	env["PRICE_BAND_EDGES"] = "100, 200,300"
	rt, err := loadRuntime(envOf(env))
	if err != nil || !reflect.DeepEqual(rt.bandEdges, []int64{100, 200, 300}) {
		t.Fatalf("edges = %v, %v", rt.bandEdges, err)
	}
	for _, bad := range []string{"x", "0", "-5", "200,100", "100,100"} {
		env["PRICE_BAND_EDGES"] = bad
		if _, err := loadRuntime(envOf(env)); err == nil || !strings.Contains(err.Error(), "PRICE_BAND_EDGES") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestShutdownDrainDelay(t *testing.T) {
	for raw, want := range map[string]time.Duration{"": DefaultShutdownDrainDelay, "0": 0, "0s": 0, "2s": 2 * time.Second, "-1s": DefaultShutdownDrainDelay, "x": DefaultShutdownDrainDelay} {
		t.Setenv("SHUTDOWN_DRAIN_DELAY", raw)
		if got := shutdownDrainDelay(quiet()); got != want {
			t.Fatalf("%q = %v, want %v", raw, got, want)
		}
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
	if getenv("PRODUCT_DISCOVERY_UNSET", "fallback") != "fallback" {
		t.Fatal("getenv fallback")
	}
	t.Setenv("PRODUCT_DISCOVERY_SET", "v")
	if getenv("PRODUCT_DISCOVERY_SET", "fallback") != "v" {
		t.Fatal("getenv value")
	}
}

func TestReadinessNeedsPostgresAndKafka(t *testing.T) {
	g := newReadiness(func(context.Context) error { return errors.New("down") }, []string{"127.0.0.1:1"})
	failing := g.Failing(context.Background())
	if len(failing) != 2 || failing[0] != "postgres" || failing[1] != "kafka" {
		t.Fatalf("failing = %v", failing)
	}
}

func TestConsumerSpecsCoverEveryGroupVariable(t *testing.T) {
	var got []string
	for _, s := range consumerSpecs() {
		got = append(got, s.groupEnv)
	}
	if !reflect.DeepEqual(got, consumerGroupEnvs) {
		t.Fatalf("specs %v vs boot-required groups %v", got, consumerGroupEnvs)
	}
}

func TestWatchWorkersReportsADeadWorker(t *testing.T) {
	alive, dead := &worker{name: "alive", done: make(chan struct{})}, &worker{name: "dead", done: make(chan struct{})}
	close(dead.done)
	select {
	case name := <-watchWorkers(alive, dead):
		if name != "dead" {
			t.Fatalf("died = %s", name)
		}
	case <-time.After(time.Second):
		t.Fatal("no worker reported")
	}
	close(alive.done)
}

func TestAwaitIsBounded(t *testing.T) {
	done := &worker{name: "w", done: make(chan struct{})}
	close(done.done)
	await(quiet(), done)
}

func serveOnce(t *testing.T, ctx context.Context, consumers []*worker) (*inboundhttp.Readiness, error) {
	t.Helper()
	t.Setenv("SHUTDOWN_DRAIN_DELAY", "0")
	readiness := inboundhttp.NewReadiness(0, 0)
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	closed := 0
	err := serveUntilSignal(ctx, quiet(), srv, readiness, consumers, func() { closed++ })
	if closed == 0 {
		t.Fatal("the consumers must be closed on every exit path")
	}
	return readiness, err
}

func TestServeUntilSignalShutsDownCleanlyWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	consumer := &worker{name: "c", done: make(chan struct{})}
	// The workers stop only AFTER the context ends, as on a real shutdown;
	// closing them first would look like a worker that died on its own.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
		time.Sleep(50 * time.Millisecond)
		close(consumer.done)
	}()
	readiness, err := serveOnce(t, ctx, []*worker{consumer})
	if err != nil || readiness.Ready() {
		t.Fatalf("err = %v ready = %v (readiness must flip first)", err, readiness.Ready())
	}
}

func TestServeUntilSignalExitsWhenAWorkerDies(t *testing.T) {
	dead := &worker{name: "pricing", done: make(chan struct{})}
	close(dead.done)
	_, err := serveOnce(t, context.Background(), []*worker{dead})
	if err == nil || !strings.Contains(err.Error(), "pricing stopped unexpectedly") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeUntilSignalReturnsAListenError(t *testing.T) {
	taken := httptest.NewServer(http.NotFoundHandler())
	defer taken.Close()
	srv := &http.Server{Addr: strings.TrimPrefix(taken.URL, "http://"), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	if err := serveUntilSignal(context.Background(), quiet(), srv, inboundhttp.NewReadiness(0, 0), nil, func() {}); err == nil {
		t.Fatal("binding a taken port must fail")
	}
}
