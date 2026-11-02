//go:build integration

package main

import (
	"testing"
	"time"

	"github.com/IQVO/product-discovery/internal/testing/kafkatest"
)

const (
	oa = "com.retailnetwork.commerce.offer-availability."
	pc = "com.retailnetwork.storefront.product-content."
	pr = "com.retailnetwork.storefront.pricing.itemprice."
)

func TestRealWiringFoldsEventsFromThreeTopicsIntoOneSearchableProduct(t *testing.T) {
	s := bootReal(t, "fold")
	kafkatest.Produce(t, topicContent, event("c1", pc+"categorytree.CategoryTreePublished", "main", map[string]any{
		"version": 1, "categories": []map[string]any{{"categoryId": "kettles", "name": "Kettles", "path": "Kettles"}},
	}))
	// Price and content arrive BEFORE the item master announces the item.
	kafkatest.Produce(t, topicPricing, event("p1", pr+"ItemPriceChanged", "I-1", map[string]any{
		"catalogItemId": "I-1", "currency": "BRL", "listPriceMinor": 12990, "effectivePriceMinor": 9990, "version": 1,
	}))
	kafkatest.Produce(t, topicContent, event("c2", pc+"productcontent.ProductContentPublished", "I-1", map[string]any{
		"catalogItemId": "I-1", "revision": 1, "description": "Boils water fast", "primaryCategoryId": "kettles",
		"media": []map[string]any{{"mediaId": "m1", "url": "https://x.test/m1.jpg", "altText": "a", "role": "PRIMARY"}},
	}))
	kafkatest.Produce(t, topicOffer,
		event("o1", oa+"catalogitem.CatalogItemCreated", "I-1", map[string]any{"catalogItemId": "I-1", "title": "Steel Kettle"}),
		event("o2", oa+"listing.ListingCreated", "L-1", map[string]any{"listingId": "L-1", "catalogItemId": "I-1", "nodeId": "N-1"}),
		event("o3", oa+"listing.ListingAvailabilityChanged", "L-1", map[string]any{
			"listingId": "L-1", "catalogItemId": "I-1", "nodeId": "N-1", "availableQty": 5, "asOf": "2026-10-25T09:00:00Z",
		}))
	eventually(t, 60*time.Second, "the product to be purchasable", func() bool {
		r := s.get(t, "/products/I-1")
		return r.status == 200 && r.json(t)["purchasable"] == true
	})
	search := s.get(t, "/products?q=kettles").json(t)
	if hits := search["items"].([]any); len(hits) != 1 || hits[0].(map[string]any)["categoryPath"].([]any)[0] != "Kettles" {
		t.Fatalf("search = %v", search)
	}
	if typo := s.get(t, "/products?q=ketle").json(t)["items"].([]any); len(typo) != 1 {
		t.Fatalf("the pg_trgm fallback must find the typo: %v", typo)
	}
	cats := s.get(t, "/categories").json(t)["categories"].([]any)
	if len(cats) != 1 || cats[0].(map[string]any)["purchasableCount"] != float64(1) {
		t.Fatalf("categories = %v", cats)
	}
	kafkatest.Produce(t, topicOffer, event("o4", oa+"listing.ListingSuppressed", "L-1", map[string]any{"listingId": "L-1"}))
	eventually(t, 60*time.Second, "the suppressed listing to empty the stock", func() bool {
		return s.get(t, "/products/I-1").json(t)["inStock"] == false
	})
}

func TestRealWiringPoisonMessageGoesToTheDeadLetterTopic(t *testing.T) {
	s := bootReal(t, "poison")
	kafkatest.Produce(t, topicPricing, event("bad1", pr+"ItemPriceChanged", "I-9", map[string]any{
		"catalogItemId": "I-9", "currency": "xx", "version": 1,
	}))
	msgs := kafkatest.ReadN(t, topicPricing+".dlq", 1, 60*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("dead letters = %d", len(msgs))
	}
	if r := s.get(t, "/products/I-9"); r.status != 404 {
		t.Fatalf("a poison fact must store nothing: %d", r.status)
	}
}
