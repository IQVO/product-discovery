// The MCP surface beyond tools (Agentic Standard v2, AS-3): ONE resource, the
// scoped product read model, and ONE prompt, this context's operator and agent
// procedure. Pinned through the same in-memory client the tool tests use.
package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const productURI = "warehouse://product-discovery/products/"

// The resource template is advertised with the fleet URI shape and reads the
// same read model as the get_product tool.
func TestSurface_ResourceTemplateListedAndRead(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	listed, err := h.session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	if len(listed.ResourceTemplates) != 1 {
		t.Fatalf("resource templates = %d, want exactly 1 (a scoped read model, never a dump)", len(listed.ResourceTemplates))
	}
	rt := listed.ResourceTemplates[0]
	if rt.URITemplate != productURI+"{catalogItemId}" || rt.MIMEType != "application/json" {
		t.Fatalf("template = %q (%q), want %q application/json", rt.URITemplate, rt.MIMEType, productURI+"{catalogItemId}")
	}

	res, err := h.session.ReadResource(ctx, &sdk.ReadResourceParams{URI: productURI + "I-1"})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d entries, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != productURI+"I-1" || c.MIMEType != "application/json" {
		t.Errorf("content = %q (%q)", c.URI, c.MIMEType)
	}
	var product wireProduct
	if err := json.Unmarshal([]byte(c.Text), &product); err != nil {
		t.Fatalf("resource text is not the product JSON: %v (%s)", err, c.Text)
	}
	if product.CatalogItemID != "I-1" || product.Title != "Steel Kettle" || !product.Purchasable || product.EffectivePrice == nil || product.EffectivePrice.AmountMinor != 3000 {
		t.Errorf("product = %+v, want the purchasable Steel Kettle at 3000", product)
	}
}

// A malformed URI, a stub and an unknown item are errors, never silent defaults.
func TestSurface_ResourceRejectsMalformedAndUnknown(t *testing.T) {
	h := newHarness(t)
	for _, uri := range []string{
		"staffing://product-discovery/products/I-1",    // wrong scheme
		"warehouse://product-discovery/categories/I-1", // wrong model
		productURI,                             // empty catalogItemId
		productURI + "I-1/extra",               // trailing segment
		productURI + "GHOST",                   // unknown item
		productURI + "S-1",                     // a stub the item master has not announced
		productURI + "I-1%00",                  // an encoded NUL
		productURI + strings.Repeat("x", 1000), // cannot name an item
	} {
		if _, err := h.session.ReadResource(t.Context(), &sdk.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("read resource %q: expected an error", uri)
		}
	}
}

// The prompt is advertised with its typed argument and returns the procedure.
func TestSurface_PromptListedAndGet(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	listed, err := h.session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(listed.Prompts) != 1 || listed.Prompts[0].Name != "find_products" {
		t.Fatalf("prompts = %+v, want exactly find_products", listed.Prompts)
	}
	if len(listed.Prompts[0].Arguments) != 1 || listed.Prompts[0].Arguments[0].Name != "query" || !listed.Prompts[0].Arguments[0].Required {
		t.Fatalf("prompt arguments = %+v, want one required query", listed.Prompts[0].Arguments)
	}

	res, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "find_products", Arguments: map[string]string{"query": "steel kettle"}})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(res.Messages))
	}
	body := res.Messages[0].Content.(*sdk.TextContent).Text
	for _, needle := range []string{"search_products", "get_product", "nextCursor", "minor units", "steel kettle"} {
		if !strings.Contains(body, needle) {
			t.Errorf("procedure text does not mention %q", needle)
		}
	}

	// A missing argument degrades to the placeholder, never to a hidden call.
	noArg, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "find_products"})
	if err != nil {
		t.Fatalf("get prompt without arguments: %v", err)
	}
	if plain := noArg.Messages[0].Content.(*sdk.TextContent).Text; !strings.Contains(plain, "<query>") {
		t.Errorf("procedure without an argument should carry the <query> placeholder")
	}
}
