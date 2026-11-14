// R1..R4 consumer contract tests for offer-availability (IQVO/offer-availability,
// OFFER_AVAILABILITY_URL): GET /catalog and GET /listings, read by cmd/rebuild.
package sources

import (
	"testing"
	"time"
)

func TestOfferAvailabilityPinnedSpecCoversEveryCallSite(t *testing.T) {
	doc := loadPinnedSpec(t, specOfferAvailability)
	// The paths come from the code's own constants, so a drift in offer_availability.go
	// re-drives this table, and a missing/renamed route in the pin fails here.
	assertSpecCoversCallSites(t, doc, []callSite{
		{method: "GET", path: pathCatalog, opID: "browseCatalog", paged: true},
		{method: "GET", path: pathListings, opID: "listListings", paged: true},
	})
}

func TestOfferAvailabilityExamplesSatisfyPinnedSpec(t *testing.T) {
	doc := loadPinnedSpec(t, specOfferAvailability)
	assertSatisfiesSchema(t, "offer-availability.catalog-page.example.json",
		operationSchema(t, doc, pathCatalog, 200, "application/json"),
		fixture(t, "offer-availability.catalog-page.example.json"))
	assertSatisfiesSchema(t, "offer-availability.listing-page.example.json",
		operationSchema(t, doc, pathListings, 200, "application/json"),
		fixture(t, "offer-availability.listing-page.example.json"))
	assertSatisfiesSchema(t, "offer-availability.problem.example.json",
		pinnedSchema(t, doc, "Problem"), fixture(t, "offer-availability.problem.example.json"))
}

func TestOfferAvailabilityGatewayDecodesCatalogExample(t *testing.T) {
	doc := loadPinnedSpec(t, specOfferAvailability)
	up := &upstream{}
	cl := serveBody(t, 200, "application/json",
		string(fixture(t, "offer-availability.catalog-page.example.json")), up)
	page, err := (OfferAvailability{Client: cl}).ListCatalog(t.Context(), "ci-4901", 2)
	if err != nil {
		t.Fatal(err)
	}
	if up.path != pathCatalog || up.query.Get("cursor") != "ci-4901" || up.query.Get("limit") != "2" {
		t.Errorf("client called %s?%s, expected %s with cursor and limit", up.path, up.query.Encode(), pathCatalog)
	}
	if len(page.Items) != 2 {
		t.Fatalf("decoded %d catalog rows, expected 2", len(page.Items))
	}
	first, second := page.Items[0], page.Items[1]
	if first.CatalogItemID != "ci-4901" || first.Title != "Stainless Steel Thermo Bottle 750 ml" || first.Hazmat || !first.Fragile || first.Discontinued {
		t.Errorf("row 0 (fragile, not hazmat, ACTIVE) decoded wrong: %+v", first)
	}
	if !second.Discontinued || !second.Hazmat {
		t.Errorf("row 1 (DISCONTINUED, hazmat) decoded wrong: %+v", second)
	}
	if page.Next != "ci-4902" {
		t.Errorf("nextCursor = %q, expected ci-4902", page.Next)
	}
	assertDecodes(t, doc, pathCatalog)
}

func TestOfferAvailabilityGatewayDecodesListingsExample(t *testing.T) {
	doc := loadPinnedSpec(t, specOfferAvailability)
	up := &upstream{}
	cl := serveBody(t, 200, "application/json",
		string(fixture(t, "offer-availability.listing-page.example.json")), up)
	page, err := (OfferAvailability{Client: cl}).ListListings(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if up.query.Has("cursor") || up.query.Has("limit") {
		t.Errorf("empty cursor/limit must not be sent, got %s", up.query.Encode())
	}
	if len(page.Items) != 2 {
		t.Fatalf("decoded %d listing rows, expected 2", len(page.Items))
	}
	live, suppressed := page.Items[0], page.Items[1]
	if live.Suppressed || live.AvailableQty != 42 || !live.AsOf.Equal(time.Date(2026, 11, 12, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("live row decoded wrong: %+v", live)
	}
	if !suppressed.Suppressed || !suppressed.AsOf.IsZero() {
		// asOf is optional in the pin ("omitted before the first feed"); the DTO
		// keeps the time.Time zero value, which must decode cleanly.
		t.Errorf("suppressed row (no asOf) decoded wrong: %+v", suppressed)
	}
	if page.Next != "" {
		t.Errorf("absent nextCursor must decode as empty, got %q", page.Next)
	}
	assertDecodes(t, doc, pathListings)
}

func TestOfferAvailabilityFieldsWeReadAreSpecified(t *testing.T) {
	doc := loadPinnedSpec(t, specOfferAvailability)
	catalogRowOptional := fieldsSpecified(t, "catalogRow", pinnedSchema(t, doc, "CatalogItem"), rowType(catalogRow{}))
	for key := range catalogRowOptional {
		t.Errorf("catalogRow reads optional %q: the pin must keep declaring it and the DTO must tolerate its absence", key)
	}
	listingRowOptional := fieldsSpecified(t, "listingRow", pinnedSchema(t, doc, "Listing"), rowType(listingRow{}))
	if _, ok := listingRowOptional["state"]; ok {
		t.Errorf("listingRow.state must stay required: the rebuild keys SUPPRESSED on it")
	}
	// availability is an anonymous struct on listingRow; check it against its own schema.
	var row listingRow
	availabilityOptional := fieldsSpecified(t, "listingRow.availability",
		pinnedSchema(t, doc, "Availability"), rowType(row.Availability))
	if !availabilityOptional["asOf"] {
		t.Errorf("asOf must stay optional in the pin: it is omitted before the first feed")
	}
	// Absence tolerance: required-only bodies must decode into every DTO we use.
	toleratesAbsence(t, "catalogRow", pinnedSchema(t, doc, "CatalogItem"), &catalogRow{})
	toleratesAbsence(t, "listingRow", pinnedSchema(t, doc, "Listing"), &listingRow{})
	toleratesAbsence(t, "listingRow.availability", pinnedSchema(t, doc, "Availability"), &row.Availability)
	toleratesAbsence(t, "page[catalogRow]", pinnedSchema(t, doc, "CatalogPage"), &page[catalogRow]{})
	toleratesAbsence(t, "page[listingRow]", pinnedSchema(t, doc, "ListingPage"), &page[listingRow]{})
}
