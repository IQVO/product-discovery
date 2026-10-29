package kafka

import (
	"context"
	"log/slog"

	ce "github.com/cloudevents/sdk-go/v2/event"

	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/productview"
)

// Full CloudEvents types of the product-content events.
const (
	TypeProductContentPublished = "com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished"
	TypeProductContentRetired   = "com.retailnetwork.storefront.product-content.productcontent.ProductContentRetired"
	TypeCategoryTreePublished   = "com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished"
)

type contentData struct {
	CatalogItemID string   `json:"catalogItemId"`
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
}

type treeData struct {
	Version    int64 `json:"version"`
	Categories []struct {
		CategoryID string `json:"categoryId"`
		ParentID   string `json:"parentId"`
		Name       string `json:"name"`
		Path       string `json:"path"`
	} `json:"categories"`
}

// NewProductContentConsumer builds the consumer of product-content's event
// topic under groupID.
func NewProductContentConsumer(brokers []string, groupID string, c usecases.Consumer, logger *slog.Logger) *Consumer {
	return newConsumer("product-content", brokers, ProductContentTopic, groupID, productContentHandlers(c), logger)
}

func productContentHandlers(c usecases.Consumer) map[string]HandlerFunc {
	return map[string]HandlerFunc{
		TypeProductContentPublished: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[contentData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyProductContentPublished(ctx, meta(e), usecases.ProductContentPublished{
				CatalogItemID: d.CatalogItemID, Content: published(d),
			}))
		},
		TypeProductContentRetired: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[contentData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyProductContentRetired(ctx, meta(e), usecases.ProductContentRetired{CatalogItemID: d.CatalogItemID, Revision: d.Revision}))
		},
		TypeCategoryTreePublished: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[treeData](e)
			if err != nil {
				return err
			}
			cats := make([]categoryview.Category, len(d.Categories))
			for i, x := range d.Categories {
				cats[i] = categoryview.Category{ID: x.CategoryID, ParentID: x.ParentID, Name: x.Name, Path: x.Path}
			}
			return classify(c.ApplyCategoryTreePublished(ctx, meta(e), usecases.CategoryTreePublished{Version: d.Version, Categories: cats}))
		},
	}
}

func published(d contentData) productview.PublishedContent {
	out := productview.PublishedContent{
		Revision: d.Revision, Description: d.Description, Bullets: d.Bullets, CategoryID: d.PrimaryCategoryID,
	}
	for _, a := range d.Attributes {
		out.Attributes = append(out.Attributes, productview.Attribute(a))
	}
	for _, m := range d.Media {
		out.Media = append(out.Media, productview.Media(m))
	}
	return out
}
