package mcp

import (
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// The views below mirror the REST wire (internal/adapters/inbound/http/dto.go)
// field for field: the MCP surface exposes exactly what GET /products and
// GET /products/{catalogItemId} expose, nothing more. They are duplicated, not
// imported, because this adapter may depend only on application and domain;
// cmd/mcp's parity test compares both surfaces so they cannot drift. They are
// also the tools' OUTPUT schema (inferred by the SDK), so every optional field
// carries omitempty.

type moneyView struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type attributeView struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Unit  *string `json:"unit,omitempty"`
}

type mediaView struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

// productView is the product page view of one catalogue item.
type productView struct {
	CatalogItemID  string          `json:"catalogItemId"`
	Title          string          `json:"title"`
	Hazmat         bool            `json:"hazmat"`
	Fragile        bool            `json:"fragile"`
	Description    string          `json:"description,omitempty"`
	Bullets        []string        `json:"bullets,omitempty"`
	Attributes     []attributeView `json:"attributes,omitempty"`
	Media          []mediaView     `json:"media,omitempty"`
	CategoryID     string          `json:"categoryId,omitempty"`
	CategoryPath   []string        `json:"categoryPath,omitempty"`
	ListPrice      *moneyView      `json:"listPrice,omitempty"`
	EffectivePrice *moneyView      `json:"effectivePrice,omitempty"`
	InStock        bool            `json:"inStock"`
	Purchasable    bool            `json:"purchasable"`
}

func toProductView(p usecases.Product) productView {
	v := p.View
	out := productView{
		CatalogItemID: v.CatalogItemID, Title: v.Title, Hazmat: v.Hazmat, Fragile: v.Fragile,
		InStock: v.InStock(), Purchasable: v.Purchasable(),
	}
	if v.Content == productview.ContentPublished {
		out.Description, out.Bullets, out.CategoryID, out.CategoryPath = v.Description, v.Bullets, v.CategoryID, p.CategoryPath
		out.Attributes, out.Media = toAttributes(v.Attributes), toMedia(v.Media)
	}
	if v.HasPrice() {
		out.ListPrice = &moneyView{AmountMinor: v.ListPriceMinor, Currency: v.Currency}
		out.EffectivePrice = &moneyView{AmountMinor: v.EffectivePriceMinor, Currency: v.Currency}
	}
	return out
}

func toAttributes(in []productview.Attribute) []attributeView {
	var out []attributeView
	for _, a := range in {
		r := attributeView{Name: a.Name, Value: a.Value}
		if a.Unit != "" {
			u := a.Unit
			r.Unit = &u
		}
		out = append(out, r)
	}
	return out
}

func toMedia(in []productview.Media) []mediaView {
	var out []mediaView
	for _, m := range in {
		out = append(out, mediaView(m))
	}
	return out
}

type hitView struct {
	CatalogItemID  string     `json:"catalogItemId"`
	Title          string     `json:"title"`
	EffectivePrice *moneyView `json:"effectivePrice,omitempty"`
	PrimaryMedia   *mediaView `json:"primaryMedia,omitempty"`
	CategoryID     string     `json:"categoryId,omitempty"`
	CategoryPath   []string   `json:"categoryPath,omitempty"`
	InStock        bool       `json:"inStock"`
	Purchasable    bool       `json:"purchasable"`
}

type facetValueView struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type facetView struct {
	Name   string           `json:"name"`
	Values []facetValueView `json:"values"`
}

type suggestionView struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
}

// searchView is one page of a search: the fleet collection shape
// {items, nextCursor} plus the facets and category suggestions of GET /products.
type searchView struct {
	Items               []hitView        `json:"items"`
	Facets              []facetView      `json:"facets"`
	CategorySuggestions []suggestionView `json:"categorySuggestions,omitempty"`
	NextCursor          string           `json:"nextCursor,omitempty"`
}

func toHitView(h usecases.HitView) hitView {
	out := hitView{
		CatalogItemID: h.CatalogItemID, Title: h.Title, CategoryID: h.CategoryID, CategoryPath: h.CategoryPath,
		InStock: h.InStock, Purchasable: h.Purchasable,
	}
	if h.HasPrice {
		out.EffectivePrice = &moneyView{AmountMinor: h.PriceMinor, Currency: h.Currency}
	}
	if h.PrimaryMedia != nil {
		m := mediaView(*h.PrimaryMedia)
		out.PrimaryMedia = &m
	}
	return out
}

func toSearchView(r usecases.SearchPage) searchView {
	out := searchView{Items: []hitView{}, Facets: toFacets(r.Facets), NextCursor: r.Next}
	for _, h := range r.Hits {
		out.Items = append(out.Items, toHitView(h))
	}
	for _, c := range r.Suggestions {
		out.CategorySuggestions = append(out.CategorySuggestions, suggestionView{CategoryID: c.ID, Name: c.Name})
	}
	return out
}

func toFacets(in []search.Facet) []facetView {
	out := make([]facetView, 0, len(in))
	for _, f := range in {
		fv := facetView{Name: f.Name, Values: []facetValueView{}}
		for _, v := range f.Values {
			fv.Values = append(fv.Values, facetValueView(v))
		}
		out = append(out, fv)
	}
	return out
}
