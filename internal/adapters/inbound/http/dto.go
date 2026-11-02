package http

import (
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

type moneyResponse struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type attributeResponse struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Unit  *string `json:"unit,omitempty"`
}

type mediaResponse struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

type productResponse struct {
	CatalogItemID  string              `json:"catalogItemId"`
	Title          string              `json:"title"`
	Hazmat         bool                `json:"hazmat"`
	Fragile        bool                `json:"fragile"`
	Description    string              `json:"description,omitempty"`
	Bullets        []string            `json:"bullets,omitempty"`
	Attributes     []attributeResponse `json:"attributes,omitempty"`
	Media          []mediaResponse     `json:"media,omitempty"`
	CategoryID     string              `json:"categoryId,omitempty"`
	CategoryPath   []string            `json:"categoryPath,omitempty"`
	ListPrice      *moneyResponse      `json:"listPrice,omitempty"`
	EffectivePrice *moneyResponse      `json:"effectivePrice,omitempty"`
	InStock        bool                `json:"inStock"`
	Purchasable    bool                `json:"purchasable"`
}

func productDTO(p usecases.Product) productResponse {
	v := p.View
	out := productResponse{
		CatalogItemID: v.CatalogItemID, Title: v.Title, Hazmat: v.Hazmat, Fragile: v.Fragile,
		InStock: v.InStock(), Purchasable: v.Purchasable(),
	}
	if v.Content == productview.ContentPublished {
		out.Description, out.Bullets, out.CategoryID, out.CategoryPath = v.Description, v.Bullets, v.CategoryID, p.CategoryPath
		out.Attributes, out.Media = attributesDTO(v.Attributes), mediaDTO(v.Media)
	}
	if v.HasPrice() {
		out.ListPrice = &moneyResponse{AmountMinor: v.ListPriceMinor, Currency: v.Currency}
		out.EffectivePrice = &moneyResponse{AmountMinor: v.EffectivePriceMinor, Currency: v.Currency}
	}
	return out
}

func attributesDTO(in []productview.Attribute) []attributeResponse {
	var out []attributeResponse
	for _, a := range in {
		r := attributeResponse{Name: a.Name, Value: a.Value}
		if a.Unit != "" {
			u := a.Unit
			r.Unit = &u
		}
		out = append(out, r)
	}
	return out
}

func mediaDTO(in []productview.Media) []mediaResponse {
	var out []mediaResponse
	for _, m := range in {
		out = append(out, mediaResponse(m))
	}
	return out
}

type hitResponse struct {
	CatalogItemID  string         `json:"catalogItemId"`
	Title          string         `json:"title"`
	EffectivePrice *moneyResponse `json:"effectivePrice,omitempty"`
	PrimaryMedia   *mediaResponse `json:"primaryMedia,omitempty"`
	CategoryID     string         `json:"categoryId,omitempty"`
	CategoryPath   []string       `json:"categoryPath,omitempty"`
	InStock        bool           `json:"inStock"`
	Purchasable    bool           `json:"purchasable"`
}

type facetValueResponse struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type facetResponse struct {
	Name   string               `json:"name"`
	Values []facetValueResponse `json:"values"`
}

type suggestionResponse struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
}

type searchResponse struct {
	Items               []hitResponse        `json:"items"`
	Facets              []facetResponse      `json:"facets"`
	CategorySuggestions []suggestionResponse `json:"categorySuggestions,omitempty"`
	NextCursor          string               `json:"nextCursor,omitempty"`
}

func hitDTO(h usecases.HitView) hitResponse {
	out := hitResponse{
		CatalogItemID: h.CatalogItemID, Title: h.Title, CategoryID: h.CategoryID, CategoryPath: h.CategoryPath,
		InStock: h.InStock, Purchasable: h.Purchasable,
	}
	if h.HasPrice {
		out.EffectivePrice = &moneyResponse{AmountMinor: h.PriceMinor, Currency: h.Currency}
	}
	if h.PrimaryMedia != nil {
		m := mediaResponse(*h.PrimaryMedia)
		out.PrimaryMedia = &m
	}
	return out
}

func searchDTO(r usecases.SearchPage) searchResponse {
	out := searchResponse{Items: []hitResponse{}, Facets: facetsDTO(r.Facets)}
	for _, h := range r.Hits {
		out.Items = append(out.Items, hitDTO(h))
	}
	for _, c := range r.Suggestions {
		out.CategorySuggestions = append(out.CategorySuggestions, suggestionResponse{CategoryID: c.ID, Name: c.Name})
	}
	out.NextCursor = r.Next
	return out
}

func facetsDTO(in []search.Facet) []facetResponse {
	out := make([]facetResponse, 0, len(in))
	for _, f := range in {
		fr := facetResponse{Name: f.Name, Values: []facetValueResponse{}}
		for _, v := range f.Values {
			fr.Values = append(fr.Values, facetValueResponse(v))
		}
		out = append(out, fr)
	}
	return out
}

type categoryResponse struct {
	CategoryID       string  `json:"categoryId"`
	ParentID         *string `json:"parentId"`
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	PurchasableCount int     `json:"purchasableCount"`
}

type categoryListResponse struct {
	Version    int64              `json:"version"`
	Categories []categoryResponse `json:"categories"`
}

func categoriesDTO(l usecases.CategoryList) categoryListResponse {
	out := categoryListResponse{Version: l.Version, Categories: []categoryResponse{}}
	for _, c := range l.Categories {
		out.Categories = append(out.Categories, categoryDTO(c))
	}
	return out
}

func categoryDTO(c categoryview.Counted) categoryResponse {
	r := categoryResponse{CategoryID: c.ID, Name: c.Name, Path: c.Path, PurchasableCount: c.PurchasableCount}
	if c.ParentID != "" {
		p := c.ParentID
		r.ParentID = &p
	}
	return r
}
