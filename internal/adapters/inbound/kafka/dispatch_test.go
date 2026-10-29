package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	ce "github.com/cloudevents/sdk-go/v2/event"
	kafkago "github.com/segmentio/kafka-go"

	kafkaconsumer "github.com/IQVO/product-discovery/internal/adapters/inbound/kafka"
	"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

var t0 = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

type noDLQ struct{}

func (noDLQ) WriteMessages(context.Context, ...kafkago.Message) error { return nil }
func (noDLQ) Close() error                                            { return nil }

// rig is the three consumers over one in-memory store.
type rig struct {
	store     *memory.Store
	queries   usecases.Queries
	consumers []*kafkaconsumer.Consumer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	s := memory.NewStore()
	deps := usecases.Deps{
		Products: s.Products(), Categories: s.Categories(), Listings: s.Listings(), Inbox: s.Inbox(),
		UoW: s.UnitOfWork(), Search: s.Search(),
	}
	c := usecases.Consumer{Deps: deps}
	mk := func(name, topic string, h map[string]kafkaconsumer.HandlerFunc) *kafkaconsumer.Consumer {
		return &kafkaconsumer.Consumer{
			Name: name, Topic: topic, Handlers: h, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			DeadLetterInvalid: true, DLQ: noDLQ{},
		}
	}
	return &rig{store: s, queries: usecases.Queries{Deps: deps}, consumers: []*kafkaconsumer.Consumer{
		mk("offer-availability", kafkaconsumer.OfferAvailabilityTopic, kafkaconsumer.OfferAvailabilityHandlers(c)),
		mk("product-content", kafkaconsumer.ProductContentTopic, kafkaconsumer.ProductContentHandlers(c)),
		mk("pricing", kafkaconsumer.PricingTopic, kafkaconsumer.PricingHandlers(c)),
	}}
}

// deliver hands the message to every consumer; a type only one of them knows
// is skipped by the others.
func (r *rig) deliver(value []byte) error {
	var errs []error
	for _, c := range r.consumers {
		errs = append(errs, c.HandleMessage(context.Background(), value))
	}
	return errors.Join(errs...)
}

// message builds a structured-mode CloudEvent of the given type.
func message(t *testing.T, id, typ string, data any) []byte {
	t.Helper()
	e := ce.New()
	e.SetID(id)
	e.SetSource("/retailnetwork/test")
	e.SetType(typ)
	e.SetSubject("subject")
	e.SetTime(t0)
	if err := e.SetData(ce.ApplicationJSON, data); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fields = map[string]any

func (r *rig) feed(t *testing.T, id, typ string, data fields) {
	t.Helper()
	if err := r.deliver(message(t, id, typ, data)); err != nil {
		t.Fatalf("%s: %v", typ, err)
	}
}
