package sources

import (
	"context"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Routes of the storefront's own services the rebuild reads.
const (
	pathItemPrices      = "/item-prices"
	pathProductContents = "/product-contents"
	pathCategories      = "/categories"
)

// Pricing reads pricing GET /item-prices.
type Pricing struct{ Client }

var _ ports.PriceSource = Pricing{}

type money struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type pricePage struct {
	Items []struct {
		CatalogItemID  string `json:"catalogItemId"`
		State          string `json:"state"`
		ListPrice      money  `json:"listPrice"`
		EffectivePrice money  `json:"effectivePrice"`
		Version        int64  `json:"version"`
	} `json:"items"`
	Next string `json:"next"`
}

// ListItemPrices implements ports.PriceSource.
func (p Pricing) ListItemPrices(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.PriceRow], error) {
	var page pricePage
	if err := p.get(ctx, pathItemPrices, pageQuery(after, limit), &page); err != nil {
		return readmodel.Page[readmodel.PriceRow]{}, err
	}
	out := readmodel.Page[readmodel.PriceRow]{Next: page.Next}
	for _, i := range page.Items {
		out.Items = append(out.Items, readmodel.PriceRow{
			CatalogItemID: i.CatalogItemID, Withdrawn: i.State == "WITHDRAWN", Currency: i.EffectivePrice.Currency,
			ListPriceMinor: i.ListPrice.AmountMinor, EffectivePriceMinor: i.EffectivePrice.AmountMinor, Version: i.Version,
		})
	}
	return out, nil
}

// ProductContent reads product-content GET /product-contents and GET /categories.
type ProductContent struct{ Client }

var _ ports.ContentSource = ProductContent{}

type contentPage struct {
	Items []struct {
		CatalogItemID string   `json:"catalogItemId"`
		State         string   `json:"state"`
		Revision      int64    `json:"revision"`
		Description   string   `json:"description"`
		Bullets       []string `json:"bullets"`
		Attributes    []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Unit  string `json:"unit"`
		} `json:"attributes"`
		Media []struct {
			MediaID string `json:"mediaId"`
			URL     string `json:"url"`
			AltText string `json:"altText"`
			Role    string `json:"role"`
		} `json:"media"`
		PrimaryCategoryID string `json:"primaryCategoryId"`
	} `json:"items"`
	Next string `json:"next"`
}

// ListProductContents implements ports.ContentSource.
func (c ProductContent) ListProductContents(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.ContentRow], error) {
	var page contentPage
	if err := c.get(ctx, pathProductContents, pageQuery(after, limit), &page); err != nil {
		return readmodel.Page[readmodel.ContentRow]{}, err
	}
	out := readmodel.Page[readmodel.ContentRow]{Next: page.Next}
	for _, i := range page.Items {
		pub := productview.PublishedContent{Revision: i.Revision, Description: i.Description, Bullets: i.Bullets, CategoryID: i.PrimaryCategoryID}
		for _, a := range i.Attributes {
			pub.Attributes = append(pub.Attributes, productview.Attribute(a))
		}
		for _, m := range i.Media {
			pub.Media = append(pub.Media, productview.Media(m))
		}
		out.Items = append(out.Items, readmodel.ContentRow{CatalogItemID: i.CatalogItemID, State: i.State, Revision: i.Revision, Published: pub})
	}
	return out, nil
}

type categoryList struct {
	Version    int64 `json:"version"`
	Categories []struct {
		CategoryID string `json:"categoryId"`
		ParentID   string `json:"parentId"`
		Name       string `json:"name"`
		Path       string `json:"path"`
	} `json:"categories"`
}

// ListCategories implements ports.ContentSource.
func (c ProductContent) ListCategories(ctx context.Context) (readmodel.CategoryTree, error) {
	var l categoryList
	if err := c.get(ctx, pathCategories, nil, &l); err != nil {
		return readmodel.CategoryTree{}, err
	}
	out := readmodel.CategoryTree{Version: l.Version}
	for _, x := range l.Categories {
		out.Categories = append(out.Categories, categoryview.Category{ID: x.CategoryID, ParentID: x.ParentID, Name: x.Name, Path: x.Path})
	}
	return out, nil
}
