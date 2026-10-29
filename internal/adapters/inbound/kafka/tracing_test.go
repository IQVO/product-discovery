package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	ce "github.com/cloudevents/sdk-go/v2/event"
	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	publisherTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	probeType            = "com.retailnetwork.commerce.test.probe.Probed"
)

func probeMessage(t *testing.T, headers []kafkago.Header) kafkago.Message {
	t.Helper()
	e := ce.New()
	e.SetID("e1")
	e.SetSource("/retailnetwork/test")
	e.SetType(probeType)
	if err := e.SetData(ce.ApplicationJSON, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return kafkago.Message{Offset: 0, Value: b, Headers: headers}
}

// tracedRun feeds one message with the given headers through Consumer.Run and
// returns the span context the handler saw and the consumer's ended spans.
// The handler fails with failWith when it is non-nil (without a DLQ it is
// retried, so the run is cancelled once the first attempt's span ended).
func tracedRun(t *testing.T, headers []kafkago.Header, failWith error) (trace.SpanContext, []sdktrace.ReadOnlySpan) {
	t.Helper()
	tp, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTracerProvider(tp); otel.SetTextMapPropagator(prop) })
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(propagation.TraceContext{})

	seen := make(chan trace.SpanContext, 8)
	rec := &loopRecorder{}
	consumer := &Consumer{
		Name: "probe", Topic: "t", Logger: quietLogger(), Retry: RetryPolicy{Initial: time.Millisecond, Max: time.Millisecond},
		Reader: &scriptedReader{rec: rec, msgs: []kafkago.Message{probeMessage(t, headers)}},
		Handlers: map[string]HandlerFunc{probeType: func(ctx context.Context, _ ce.Event) error {
			seen <- trace.SpanContextFromContext(ctx)
			return failWith
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for len(recorder.Ended()) == 0 {
		select {
		case <-deadline:
			t.Fatal("no consumer span ended")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	return <-seen, recorder.Ended()
}

// The consumer span is a consumer-kind span named "<topic> process" whose
// parent is the publisher's span carried in traceparent, and the handler runs
// inside it.
func TestConsumerSpanIsParentedToThePublisherTrace(t *testing.T) {
	seen, spans := tracedRun(t, []kafkago.Header{
		{Key: "content-type", Value: []byte("application/cloudevents+json; charset=UTF-8")},
		{Key: "traceparent", Value: []byte(publisherTraceparent)},
	}, nil)

	span := spans[0]
	if span.SpanKind() != trace.SpanKindConsumer || span.Name() != "t process" {
		t.Errorf("span = %v %q, want consumer \"t process\"", span.SpanKind(), span.Name())
	}
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %s, want the publisher's", got)
	}
	if got := span.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent span id = %s, want the publish span's", got)
	}
	if seen.SpanID() != span.SpanContext().SpanID() {
		t.Errorf("handler ran under span %s, want the consumer span %s", seen.SpanID(), span.SpanContext().SpanID())
	}
}

func TestConsumerWithoutTraceHeadersStartsANewTrace(t *testing.T) {
	_, spans := tracedRun(t, nil, nil)
	if spans[0].Parent().IsValid() {
		t.Errorf("parent = %v, want none for a message without traceparent", spans[0].Parent())
	}
}

func TestConsumerSpanRecordsAHandlerFailure(t *testing.T) {
	_, spans := tracedRun(t, []kafkago.Header{{Key: "traceparent", Value: []byte(publisherTraceparent)}}, errors.New("boom"))
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("status = %v, want an errored consumer span", spans[0].Status())
	}
}

func TestHeaderCarrierReadsAndNeverWrites(t *testing.T) {
	c := headerCarrier{headers: []kafkago.Header{{Key: "traceparent", Value: []byte("x")}, {Key: "k", Value: []byte("v")}}}
	c.Set("traceparent", "y")
	if c.Get("traceparent") != "x" || c.Get("absent") != "" {
		t.Errorf("Get = %q / %q", c.Get("traceparent"), c.Get("absent"))
	}
	if keys := c.Keys(); len(keys) != 2 || keys[0] != "traceparent" || keys[1] != "k" {
		t.Errorf("Keys = %v", keys)
	}
}
