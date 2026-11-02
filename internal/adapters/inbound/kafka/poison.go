package kafka

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meterName scopes this service's own instruments, keeping them distinct from
// the ones otelchi and the runtime collector register (same value as the
// telemetry adapter's, which is unexported there).
const meterName = "github.com/IQVO/product-discovery"

// poisonMessagesName counts the deterministic poison messages the consumers
// meet: everything the run loop commits past (or dead-letters) because it can
// never be applied. Without it those paths are only log lines.
const poisonMessagesName = "discovery.kafka.poison_messages"

// Dispositions (what the run loop did) and reasons (why the message can never
// be applied) carried as counter attributes.
const (
	dispositionSkipped      = "skipped"
	dispositionDeadLettered = "dead-lettered"

	reasonInvalidEnvelope    = "invalid-envelope"
	reasonUnknownType        = "unknown-type"
	reasonUnappliablePayload = "unappliable-payload"
)

// PoisonCounter counts one deterministic poison message, attributed by
// consumer name, disposition (skipped or dead-lettered) and reason, so a
// silently committed message is observable and alertable.
type PoisonCounter interface {
	Add(ctx context.Context, consumer, disposition, reason string)
}

// newPoisonCounter registers the OTel counter against the global
// MeterProvider. Until telemetry.Setup installs a real provider the global
// one is a no-op, so recording is cheap and safe in tests and local runs;
// the composition root builds the consumers after Setup, which binds them
// to the real provider.
func newPoisonCounter() PoisonCounter {
	counter, err := otel.Meter(meterName).Int64Counter(
		poisonMessagesName,
		metric.WithDescription("Deterministic poison messages by consumer, disposition and reason. "+
			"skipped means the message was committed past (invalid-envelope: not a CloudEvents 1.0 message; "+
			"unknown-type: a type this consumer does not handle; unappliable-payload: a handled type whose data "+
			"can never be applied and dead-lettering is off). dead-lettered/unappliable-payload means it was "+
			"published to the topic's .dlq after its bounded retries. Anything above zero on skipped deserves an "+
			"alert: those messages are gone from the projection forever."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		// Only an invalid instrument name can fail, and the name is a
		// constant above; degrade to a no-op rather than panic.
		return noopPoisonCounter{}
	}
	return otelPoisonCounter{counter: counter}
}

// otelPoisonCounter implements PoisonCounter against the global MeterProvider.
type otelPoisonCounter struct{ counter metric.Int64Counter }

// Add counts one poison message.
func (c otelPoisonCounter) Add(ctx context.Context, consumer, disposition, reason string) {
	c.counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("consumer", consumer),
		attribute.String("disposition", disposition),
		attribute.String("reason", reason),
	))
}

// noopPoisonCounter counts nothing; the fallback for a nil counter.
type noopPoisonCounter struct{}

// Add is a no-op.
func (noopPoisonCounter) Add(context.Context, string, string, string) {}
