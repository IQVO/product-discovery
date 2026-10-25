package kafka

import (
	"errors"

	ce "github.com/cloudevents/sdk-go/v2/event"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// Topics product-discovery consumes (ADR 0003).
const (
	OfferAvailabilityTopic = "retailnetwork.offer-availability.events"
	ProductContentTopic    = "retailnetwork.product-content.events"
	PricingTopic           = "retailnetwork.pricing.events"
)

// meta is what the use cases need to know about the envelope.
func meta(e ce.Event) usecases.Meta {
	return usecases.Meta{EventID: e.ID(), Type: e.Type(), At: e.Time()}
}

// classify turns a fact that can never be folded (an id the projection cannot
// hold, a negative quantity) into a poison payload, dead-lettered at once;
// every other error is transient and retried.
func classify(err error) error {
	if errors.Is(err, shared.ErrInvalidFact) {
		return errors.Join(errBadPayload, err)
	}
	return err
}
