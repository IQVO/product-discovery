package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"
	inboundkafka "github.com/IQVO/product-discovery/internal/adapters/inbound/kafka"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// worker is a started background loop and the channel closed once it returned.
type worker struct {
	name string
	done chan struct{}
}

// consumerSpec is one consumer: its group variable and how to build it once the
// group is resolved.
type consumerSpec struct {
	groupEnv string
	build    func(c usecases.Consumer, brokers []string, group string, logger *slog.Logger) *inboundkafka.Consumer
}

func consumerSpecs() []consumerSpec {
	return []consumerSpec{
		{"OFFER_AVAILABILITY_CONSUMER_GROUP", func(c usecases.Consumer, brokers []string, group string, logger *slog.Logger) *inboundkafka.Consumer {
			return inboundkafka.NewOfferAvailabilityConsumer(brokers, group, c, logger)
		}},
		{"PRODUCT_CONTENT_CONSUMER_GROUP", func(c usecases.Consumer, brokers []string, group string, logger *slog.Logger) *inboundkafka.Consumer {
			return inboundkafka.NewProductContentConsumer(brokers, group, c, logger)
		}},
		{"PRICING_CONSUMER_GROUP", func(c usecases.Consumer, brokers []string, group string, logger *slog.Logger) *inboundkafka.Consumer {
			return inboundkafka.NewPricingConsumer(brokers, group, c, logger)
		}},
	}
}

// startConsumers starts every consumer; they are always on. The readers dial
// lazily inside Run, so a broker outage never blocks boot.
func startConsumers(deps usecases.Deps, rt runtimeEnv, logger *slog.Logger) ([]*worker, func()) {
	ctx, stop := context.WithCancel(context.Background())
	var (
		workers []*worker
		closers []func() error
	)
	for _, spec := range consumerSpecs() {
		group := rt.groups[spec.groupEnv]
		consumer := spec.build(usecases.Consumer{Deps: deps}, rt.brokers, group, logger)
		w := &worker{name: consumer.Name, done: make(chan struct{})}
		workers = append(workers, w)
		closers = append(closers, consumer.Close)
		go func() {
			defer close(w.done)
			logger.Info("consumer running", "consumer", consumer.Name, "topic", consumer.Topic, "group_id", group, "brokers", rt.brokers)
			if err := consumer.Run(ctx); !errors.Is(err, context.Canceled) {
				logger.Error("consumer stopped", "consumer", consumer.Name, "error", err)
			}
		}()
	}
	return workers, func() {
		stop()
		for _, closeFn := range closers {
			_ = closeFn()
		}
	}
}

// serveUntilSignal runs httpServer until SIGINT/SIGTERM (or a listen error),
// then shuts down in the fleet order: (1) /readyz flips to 503, (2) wait the
// drain delay, (3) drain HTTP, (4) stop and await the consumers; the caller's
// deferred pool.Close runs after all of it.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, httpServer *http.Server, readiness *inboundhttp.Readiness,
	consumers []*worker, closeConsumers func(),
) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	died := watchWorkers(consumers...)
	select {
	case err := <-errCh:
		closeConsumers()
		return err
	case name := <-died:
		// A consumer returned on its own (a lost broker, a failed reader): the
		// service cannot run without it and must not sit "alive" with a dead
		// worker, so exit non-zero and let the orchestrator restart it.
		closeConsumers()
		return fmt.Errorf("%s stopped unexpectedly: exiting so the orchestrator restarts the service", name)
	case <-sigCtx.Done():
	}

	readiness.SetNotReady()
	if delay := shutdownDrainDelay(logger); delay > 0 {
		logger.Info("shutdown: readiness flipped to not-ready; waiting for traffic to drain", "drain_delay", delay.String())
		time.Sleep(delay)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := httpServer.Shutdown(shutdownCtx)

	closeConsumers()
	for _, w := range consumers {
		await(logger, w)
	}
	return err
}

// watchWorkers returns a channel that receives the name of every worker that
// returns. Workers only return when stopped or on an unrecoverable failure, so
// a value read while the service is serving means a worker died.
func watchWorkers(workers ...*worker) <-chan string {
	died := make(chan string, len(workers))
	for _, w := range workers {
		go func() {
			<-w.done
			died <- w.name
		}()
	}
	return died
}

// await waits (bounded) for w to return.
func await(logger *slog.Logger, w *worker) {
	select {
	case <-w.done:
	case <-time.After(workerDrainTimeout):
		logger.Warn("worker did not stop before the shutdown drain deadline", "worker", w.name)
	}
}
