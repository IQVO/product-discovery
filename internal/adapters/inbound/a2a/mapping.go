package a2a

import (
	"fmt"
	"net/http"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/productview"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// problemBase is the namespace of this service's RFC 9457 `type` URIs. It is
// the SAME namespace and the same slugs as the REST adapter's, so a delegating
// agent sees the identifiers a REST caller would for the outcomes both
// surfaces share.
const problemBase = "/problems/"

// problem is the RFC 7807-shaped rejection an agent client receives instead of
// a result: domain rejections and request-shape failures are business
// answers, not transport errors, so they travel in the message envelope.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// summary renders the one-line form used for the reply's text part.
func (p *problem) summary() string {
	return fmt.Sprintf("%s (%d): %s", p.Title, p.Status, p.Detail)
}

// The four problem classes of the REST adapter's catalogue for these reads
// (internal/adapters/inbound/http/errors.go): malformed-request 400,
// not-found 404, validation-failed 422 and internal-error 500. There is no
// 409 (no write) and no 503 (the REST adapter has none for these reads).
func problemMalformed(detail string) *problem {
	return &problem{Type: problemBase + "malformed-request", Title: "Malformed request", Status: http.StatusBadRequest, Detail: detail}
}

func problemNotFound(detail string) *problem {
	return &problem{Type: problemBase + "not-found", Title: "Not found", Status: http.StatusNotFound, Detail: detail}
}

func problemValidation(detail string) *problem {
	return &problem{Type: problemBase + "validation-failed", Title: "Validation failed", Status: http.StatusUnprocessableEntity, Detail: detail}
}

func problemInternal() *problem {
	return &problem{Type: problemBase + "internal-error", Title: "Internal server error", Status: http.StatusInternalServerError, Detail: "an unexpected error occurred"}
}

func problemUnknownSkill(skill string) *problem {
	return problemMalformed(fmt.Sprintf("skill %q is not offered by this agent; the Agent Card lists the supported skills", skill))
}

// envelope is the structured payload of every skill answer: exactly one of
// Data (a skill result) or Problem (why there is no result), plus the skill id
// the answer belongs to (empty when the request named none that parsed).
type envelope struct {
	Skill   string     `json:"skill"`
	Data    *skillData `json:"data,omitempty"`
	Problem *problem   `json:"problem,omitempty"`
}

// skillData is the union of the skill results; each skill fills its own field.
type skillData struct {
	Product *productResponse `json:"product,omitempty"`
	Search  *searchResponse  `json:"search,omitempty"`
}

// The DTOs below mirror the REST wire (internal/adapters/inbound/http/dto.go)
// field for field: the A2A surface exposes exactly what GET /products and
// GET /products/{catalogItemId} expose, nothing more. They are duplicated, not
// imported, because this adapter may depend only on application and domain;
// cmd/a2a's parity test compares both surfaces so they cannot drift.

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
	Hits                []hitResponse        `json:"hits"`
	Facets              []facetResponse      `json:"facets"`
	CategorySuggestions []suggestionResponse `json:"categorySuggestions,omitempty"`
	Next                *string              `json:"next"`
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
	out := searchResponse{Hits: []hitResponse{}, Facets: facetsDTO(r.Facets)}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, hitDTO(h))
	}
	for _, c := range r.Suggestions {
		out.CategorySuggestions = append(out.CategorySuggestions, suggestionResponse{CategoryID: c.ID, Name: c.Name})
	}
	if r.Next != "" {
		n := r.Next
		out.Next = &n
	}
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

// summarizeProduct is the one-line text form of one product.
func summarizeProduct(p productResponse) string {
	state := "not purchasable"
	if p.Purchasable {
		state = "purchasable"
	}
	price := "no price yet"
	if p.EffectivePrice != nil {
		price = fmt.Sprintf("effective price %d %s (minor units)", p.EffectivePrice.AmountMinor, p.EffectivePrice.Currency)
	}
	return fmt.Sprintf("%s: %q, %s, %s", p.CatalogItemID, p.Title, state, price)
}

// summarizeSearch is the one-line text form of one search page.
func summarizeSearch(s searchResponse) string {
	more := "last page"
	if s.Next != nil {
		more = "more pages: pass next as after"
	}
	return fmt.Sprintf("%d products on this page (%s)", len(s.Hits), more)
}
