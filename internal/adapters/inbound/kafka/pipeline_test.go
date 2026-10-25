package kafka_test

import (
	"context"
	"errors"
	"testing"

	kafkaconsumer "github.com/IQVO/product-discovery/internal/adapters/inbound/kafka"
)

func TestElevenEventTypesFoldIntoOnePurchasableProduct(t *testing.T) {
	r := newRig(t)
	r.feed(t, "e1", kafkaconsumer.TypeCategoryTreePublished, fields{"version": 3, "categories": []fields{
		{"categoryId": "home", "name": "Home", "path": "Home"},
		{"categoryId": "kettles", "parentId": "home", "name": "Kettles", "path": "Home/Kettles"},
	}})
	r.feed(t, "e2", kafkaconsumer.TypeCatalogItemCreated, fields{"catalogItemId": "I1", "title": "Steel Kettle", "hazmat": false, "fragile": true})
	r.feed(t, "e3", kafkaconsumer.TypeListingCreated, fields{"listingId": "L1", "catalogItemId": "I1", "nodeId": "N1"})
	r.feed(t, "e4", kafkaconsumer.TypeListingAvailabilityChanged, fields{
		"listingId": "L1", "catalogItemId": "I1", "nodeId": "N1", "availableQty": 7, "asOf": "2026-10-25T09:00:00Z",
	})
	r.feed(t, "e5", kafkaconsumer.TypeProductContentPublished, fields{
		"catalogItemId": "I1", "revision": 2, "description": "Boils", "bullets": []string{"fast"}, "primaryCategoryId": "kettles",
		"attributes": []fields{{"name": "capacity", "value": "1.7", "unit": "l"}},
		"media":      []fields{{"mediaId": "m1", "url": "https://x.test/m1.jpg", "altText": "a", "role": "PRIMARY"}},
	})
	r.feed(t, "e6", kafkaconsumer.TypeItemPriceChanged, fields{
		"catalogItemId": "I1", "currency": "BRL", "listPriceMinor": 12990, "effectivePriceMinor": 9990, "version": 7,
	})
	p, err := r.queries.GetProduct(context.Background(), "I1")
	if err != nil {
		t.Fatal(err)
	}
	v := p.View
	if !v.Purchasable() || !v.Fragile || v.Description != "Boils" || v.Attributes[0].Unit != "l" || v.Media[0].MediaID != "m1" ||
		v.ListPriceMinor != 12990 || v.EffectivePriceMinor != 9990 || len(p.CategoryPath) != 2 {
		t.Fatalf("view = %+v path %v", v, p.CategoryPath)
	}
	r.feed(t, "e7", kafkaconsumer.TypeListingSuppressed, fields{"listingId": "L1", "nodeId": "N1", "reason": "NODE_NOT_SELLABLE"})
	if p, _ := r.queries.GetProduct(context.Background(), "I1"); p.View.InStock() {
		t.Fatal("ListingSuppressed must take the item out of stock")
	}
	r.feed(t, "e8", kafkaconsumer.TypeListingRestored, fields{"listingId": "L1", "nodeId": "N1"})
	r.feed(t, "e9", kafkaconsumer.TypeItemPriceWithdrawn, fields{"catalogItemId": "I1", "version": 8})
	r.feed(t, "e10", kafkaconsumer.TypeProductContentRetired, fields{"catalogItemId": "I1", "revision": 2})
	r.feed(t, "e11", kafkaconsumer.TypeCatalogItemDiscontinued, fields{"catalogItemId": "I1"})
	p, _ = r.queries.GetProduct(context.Background(), "I1")
	if p.View.Purchasable() || p.View.Item != "DISCONTINUED" || !p.View.InStock() {
		t.Fatalf("after the last five events: %+v", p.View)
	}
}

func TestDuplicateDeliveryIsHarmless(t *testing.T) {
	r := newRig(t)
	msg := message(t, "dup", kafkaconsumer.TypeCatalogItemCreated, fields{"catalogItemId": "I1", "title": "Once"})
	for i := 0; i < 3; i++ {
		if err := r.deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.store.Counts(); n.Products != 1 || n.Inbox != 1 {
		t.Fatalf("counts = %+v", n)
	}
}

func TestUnfoldableFactsArePoison(t *testing.T) {
	r := newRig(t)
	bad := map[string][2]any{
		"bad id":         {kafkaconsumer.TypeCatalogItemCreated, fields{"catalogItemId": "bad id", "title": "x"}},
		"no title":       {kafkaconsumer.TypeCatalogItemCreated, fields{"catalogItemId": "I1"}},
		"negative qty":   {kafkaconsumer.TypeListingAvailabilityChanged, fields{"listingId": "L", "catalogItemId": "I1", "nodeId": "N", "availableQty": -3, "asOf": "2026-10-25T09:00:00Z"}},
		"bad currency":   {kafkaconsumer.TypeItemPriceChanged, fields{"catalogItemId": "I1", "currency": "xx", "version": 1}},
		"tree version 0": {kafkaconsumer.TypeCategoryTreePublished, fields{"version": 0}},
		"bad listing":    {kafkaconsumer.TypeListingSuppressed, fields{"listingId": "bad id"}},
	}
	for name, c := range bad {
		err := r.deliver(message(t, "bad-"+name, c[0].(string), c[1]))
		if !errors.Is(err, kafkaconsumer.ErrPoison) {
			t.Errorf("%s: err = %v, want poison", name, err)
		}
	}
	if n := r.store.Counts(); n.Products != 0 || n.Inbox != 0 {
		t.Fatalf("poison must store nothing: %+v", n)
	}
}

func TestMalformedDataTypesArePoisonAndUnknownThingsAreSkipped(t *testing.T) {
	r := newRig(t)
	err := r.deliver(message(t, "m1", kafkaconsumer.TypeItemPriceChanged, fields{"catalogItemId": "I1", "version": "seven"}))
	if !errors.Is(err, kafkaconsumer.ErrPoison) {
		t.Fatalf("a string version: %v", err)
	}
	for name, raw := range map[string][]byte{
		"not json":     []byte("hello"),
		"unknown type": message(t, "u1", "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemRenamed", fields{}),
		"suffix only":  message(t, "u2", "CatalogItemCreated", fields{"catalogItemId": "I1", "title": "x"}),
	} {
		if err := r.deliver(raw); err != nil {
			t.Errorf("%s must be skipped, got %v", name, err)
		}
	}
	if r.store.Counts().Products != 0 {
		t.Fatal("skipped messages store nothing")
	}
}
