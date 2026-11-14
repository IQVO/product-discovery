package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mimeTypeJSON is the MIME type of every resource this server serves.
const mimeTypeJSON = "application/json"

// productURIPrefix is the fleet resource URI shape of this context.
const productURIPrefix = "warehouse://product-discovery/products/"

// productURITemplate is the ONE resource this server offers (Agentic Standard
// v2, AS-3): a scoped read model this context owns, not a dump of the
// catalogue. It answers "what does this product look like on its page?" for ONE
// item, built from the same GetProduct read use case the get_product tool and
// GET /products/{catalogItemId} use. Bounded by construction: one item, the
// product view's fixed fields, JSON text.
const productURITemplate = productURIPrefix + "{catalogItemId}"

// registerResources adds the resource surface (read-only by nature, L1; the
// charter keeps resources context data an agent can load without a tool
// call). No resource ever writes: Deps holds read use cases only (ADR 0009).
func (d Deps) registerResources(server *mcp.Server) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: productURITemplate,
		Name:        "product page view",
		Description: "The product page view of one catalogue item: title, hazmat and fragile flags, description, bullets, attributes, media, category path, list and effective price in minor units, inStock and purchasable. An unknown item, or one the item master has not announced yet, is an error.",
		MIMEType:    mimeTypeJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, err := parseProductURI(req.Params.URI)
		if err != nil {
			return nil, err
		}
		product, err := d.lookupProduct(ctx, id)
		switch {
		case isNotFound(err):
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		case err != nil:
			return nil, mapError(ctx, err)
		}
		return resourceResult(req.Params.URI, toProductView(product))
	})
}

// resourceResult marshals one read model into the resource's text contents.
func resourceResult(uri string, view any) (*mcp.ReadResourceResult, error) {
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: mimeTypeJSON,
			Text:     string(body),
		}},
	}, nil
}

// parseProductURI extracts the catalogItemId from a
// warehouse://product-discovery/products/{catalogItemId} URI. The id is
// model-supplied and therefore untrusted: a malformed URI or an empty id is
// rejected rather than silently defaulted.
func parseProductURI(uri string) (string, error) {
	rest, ok := strings.CutPrefix(uri, productURIPrefix)
	if !ok {
		return "", fmt.Errorf("resource uri %q must start with %q", truncate(uri), productURIPrefix)
	}
	if rest == "" || strings.Contains(rest, "/") {
		return "", fmt.Errorf("resource uri %q must carry exactly one non-empty catalogItemId after %q", truncate(uri), productURIPrefix)
	}
	id, err := url.PathUnescape(rest)
	if err != nil {
		return "", fmt.Errorf("resource uri %q carries an invalid escape: %w", truncate(uri), err)
	}
	return id, nil
}
