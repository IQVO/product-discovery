// R1..R4 consumer contract tests for pricing (IQVO/pricing, PRICING_URL):
// GET /item-prices, read by cmd/rebuild.
package sources

import (
	"testing"
)

func TestPricingPinnedSpecCoversEveryCallSite(t *testing.T) {
	doc := loadPinnedSpec(t, specPricing)
	assertSpecCoversCallSites(t, doc, []callSite{
		{method: "GET", path: pathItemPrices, opID: "listItemPrices", paged: true},
	})
}

func TestPricingExamplesSatisfyPinnedSpec(t *testing.T) {
	doc := loadPinnedSpec(t, specPricing)
	assertSatisfiesSchema(t, "pricing.item-price-list.example.json",
		operationSchema(t, doc, pathItemPrices, 200, "application/json"),
		fixture(t, "pricing.item-price-list.example.json"))
	// pricing's Problem schema is stricter than the siblings': detail and
	// instance are required too, so the fixture carries them.
	assertSatisfiesSchema(t, "pricing.problem.example.json",
		pinnedSchema(t, doc, "Problem"), fixture(t, "pricing.problem.example.json"))
}

func TestPricingGatewayDecodesSpecExamples(t *testing.T) {
	doc := loadPinnedSpec(t, specPricing)
	up := &upstream{}
	cl := serveBody(t, 200, "application/json",
		string(fixture(t, "pricing.item-price-list.example.json")), up)
	page, err := (Pricing{Client: cl}).ListItemPrices(t.Context(), "ci-4901", 500)
	if err != nil {
		t.Fatal(err)
	}
	if up.path != pathItemPrices || up.query.Get("cursor") != "ci-4901" || up.query.Get("limit") != "500" {
		t.Errorf("client called %s?%s, expected %s with cursor and limit", up.path, up.query.Encode(), pathItemPrices)
	}
	if len(page.Items) != 2 {
		t.Fatalf("decoded %d price rows, expected 2", len(page.Items))
	}
	active, withdrawn := page.Items[0], page.Items[1]
	if active.CatalogItemID != "ci-4901" || active.Withdrawn || active.ListPriceMinor != 14990 ||
		active.EffectivePriceMinor != 12990 || active.Currency != "BRL" || active.Version != 3 {
		t.Errorf("ACTIVE row decoded wrong: %+v", active)
	}
	if !withdrawn.Withdrawn || withdrawn.Version != 7 {
		t.Errorf("WITHDRAWN row decoded wrong: %+v", withdrawn)
	}
	if page.Next != "ci-4902" {
		t.Errorf("nextCursor = %q, expected ci-4902", page.Next)
	}
	assertDecodes(t, doc, pathItemPrices)
}

func TestPricingFieldsWeReadAreSpecified(t *testing.T) {
	doc := loadPinnedSpec(t, specPricing)
	optional := fieldsSpecified(t, "priceRow", pinnedSchema(t, doc, "ItemPrice"), rowType(priceRow{}))
	for key := range optional {
		t.Errorf("priceRow reads optional %q: pricing must keep declaring it and the DTO must tolerate its absence", key)
	}
	moneyOptional := fieldsSpecified(t, "priceRow.money", pinnedSchema(t, doc, "Money"), rowType(money{}))
	for key := range moneyOptional {
		t.Errorf("money reads optional %q, which the pin does not require", key)
	}
	toleratesAbsence(t, "priceRow", pinnedSchema(t, doc, "ItemPrice"), &priceRow{})
	toleratesAbsence(t, "priceRow.money", pinnedSchema(t, doc, "Money"), &money{})
	toleratesAbsence(t, "page[priceRow]", pinnedSchema(t, doc, "ItemPriceList"), &page[priceRow]{})
}
