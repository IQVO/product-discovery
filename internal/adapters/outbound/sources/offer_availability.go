package sources

import (
	"context"
	"time"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/readmodel"
)

// Routes of offer-availability the rebuild reads. GET /catalog is an Open Host
// Service read; GET /listings is the ops-only in-org exception (the listing
// availability has no storefront OHS read), used only by cmd/rebuild.
const (
	pathCatalog  = "/catalog"
	pathListings = "/listings"
)

// OfferAvailability reads offer-availability.
type OfferAvailability struct{ Client }

var (
	_ ports.CatalogSource = OfferAvailability{}
	_ ports.ListingSource = OfferAvailability{}
)

type catalogPage struct {
	Items []struct {
		CatalogItemID string `json:"catalogItemId"`
		Title         string `json:"title"`
		Hazmat        bool   `json:"hazmat"`
		Fragile       bool   `json:"fragile"`
		State         string `json:"state"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// ListCatalog implements ports.CatalogSource.
func (o OfferAvailability) ListCatalog(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.CatalogRow], error) {
	var p catalogPage
	if err := o.get(ctx, pathCatalog, pageQuery(after, limit), &p); err != nil {
		return readmodel.Page[readmodel.CatalogRow]{}, err
	}
	out := readmodel.Page[readmodel.CatalogRow]{Next: p.NextCursor}
	for _, i := range p.Items {
		out.Items = append(out.Items, readmodel.CatalogRow{
			CatalogItemID: i.CatalogItemID, Title: i.Title, Hazmat: i.Hazmat, Fragile: i.Fragile, Discontinued: i.State == "DISCONTINUED",
		})
	}
	return out, nil
}

type listingPage struct {
	Listings []struct {
		ListingID     string `json:"listingId"`
		CatalogItemID string `json:"catalogItemId"`
		NodeID        string `json:"nodeId"`
		State         string `json:"state"`
		Availability  struct {
			AvailableQty int64     `json:"availableQty"`
			AsOf         time.Time `json:"asOf"`
		} `json:"availability"`
	} `json:"listings"`
	NextCursor string `json:"nextCursor"`
}

// ListListings implements ports.ListingSource.
func (o OfferAvailability) ListListings(ctx context.Context, after string, limit int) (readmodel.Page[readmodel.ListingRow], error) {
	var p listingPage
	if err := o.get(ctx, pathListings, pageQuery(after, limit), &p); err != nil {
		return readmodel.Page[readmodel.ListingRow]{}, err
	}
	out := readmodel.Page[readmodel.ListingRow]{Next: p.NextCursor}
	for _, l := range p.Listings {
		out.Items = append(out.Items, readmodel.ListingRow{
			ListingID: l.ListingID, CatalogItemID: l.CatalogItemID, NodeID: l.NodeID, Suppressed: l.State == "SUPPRESSED",
			AvailableQty: l.Availability.AvailableQty, AsOf: l.Availability.AsOf.UTC(),
		})
	}
	return out, nil
}
