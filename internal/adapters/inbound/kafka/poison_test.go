package kafka_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	kafkaconsumer "github.com/IQVO/product-discovery/internal/adapters/inbound/kafka"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// recordingPoison records every Add call.
type recordingPoison struct{ calls [][3]string }

func (p *recordingPoison) Add(_ context.Context, consumer, disposition, reason string) {
	p.calls = append(p.calls, [3]string{consumer, disposition, reason})
}

// poisonConsumer builds one offer-availability consumer whose counter is p.
func poisonConsumer(t *testing.T, p *recordingPoison, deadLetterInvalid bool) *kafkaconsumer.Consumer {
	t.Helper()
	s := memory.NewStore()
	deps := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	return &kafkaconsumer.Consumer{
		Name: "offer-availability", Topic: kafkaconsumer.OfferAvailabilityTopic,
		Handlers:          kafkaconsumer.OfferAvailabilityHandlers(usecases.Consumer{Deps: deps}),
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		DeadLetterInvalid: deadLetterInvalid, DLQ: noDLQ{}, Poison: p,
	}
}

// TestSilentSkipsAreCounted proves the two paths the run loop commits past
// without any other trace (a non-CloudEvent message and an unknown type)
// increment the poison counter.
func TestSilentSkipsAreCounted(t *testing.T) {
	p := &recordingPoison{}
	c := poisonConsumer(t, p, true)
	if err := c.HandleMessage(context.Background(), []byte("hello")); err != nil {
		t.Fatalf("invalid envelope: %v", err)
	}
	unknown := message(t, "u1", "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemRenamed", fields{})
	if err := c.HandleMessage(context.Background(), unknown); err != nil {
		t.Fatalf("unknown type: %v", err)
	}
	want := [][3]string{
		{"offer-availability", "skipped", "invalid-envelope"},
		{"offer-availability", "skipped", "unknown-type"},
	}
	if len(p.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
	for i, w := range want {
		if p.calls[i] != w {
			t.Fatalf("call %d = %v, want %v", i, p.calls[i], w)
		}
	}
}

// TestDeadLetteredPoisonIsCounted proves an unappliable payload of a handled
// type counts as dead-lettered when the DLQ path is on, and as skipped when
// it is off (the config-off fallback must not go silent either).
func TestDeadLetteredPoisonIsCounted(t *testing.T) {
	bad := message(t, "bad-1", kafkaconsumer.TypeCatalogItemCreated, fields{"catalogItemId": "bad id", "title": "x"})

	p := &recordingPoison{}
	c := poisonConsumer(t, p, true)
	if err := c.HandleMessage(context.Background(), bad); !errors.Is(err, kafkaconsumer.ErrPoison) {
		t.Fatalf("err = %v, want poison", err)
	}
	if len(p.calls) != 1 || p.calls[0] != [3]string{"offer-availability", "dead-lettered", "unappliable-payload"} {
		t.Fatalf("calls = %v", p.calls)
	}

	p2 := &recordingPoison{}
	c2 := poisonConsumer(t, p2, false)
	if err := c2.HandleMessage(context.Background(), bad); err != nil {
		t.Fatalf("config-off poison must be skipped, got %v", err)
	}
	if len(p2.calls) != 1 || p2.calls[0] != [3]string{"offer-availability", "skipped", "unappliable-payload"} {
		t.Fatalf("calls = %v", p2.calls)
	}
}

// TestNewPoisonCounterRegistersAnOTelCounter proves the real counter binds to
// the global MeterProvider and carries the consumer/disposition/reason
// attributes an alert would filter on.
func TestNewPoisonCounterRegistersAnOTelCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		_ = mp.Shutdown(context.Background())
	})

	kafkaconsumer.NewPoisonCounter().Add(context.Background(), "pricing", "skipped", "unknown-type")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, m := range rm.ScopeMetrics {
		for _, mm := range m.Metrics {
			if mm.Name != "discovery.kafka.poison_messages" {
				continue
			}
			sum, ok := mm.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
				t.Fatalf("metric %s data = %+v", mm.Name, mm.Data)
			}
			attrs := sum.DataPoints[0].Attributes
			for _, want := range [][2]string{{"consumer", "pricing"}, {"disposition", "skipped"}, {"reason", "unknown-type"}} {
				got, _ := attrs.Value(attribute.Key(want[0]))
				if got.AsString() != want[1] {
					t.Fatalf("attribute %s = %q, want %q", want[0], got.AsString(), want[1])
				}
			}
			return
		}
	}
	t.Fatal("discovery.kafka.poison_messages was not registered")
}
