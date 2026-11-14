// R1..R4 consumer contract tests for product-content (IQVO/product-content,
// PRODUCT_CONTENT_URL): GET /product-contents and GET /categories, read by
// cmd/rebuild. /categories is pagination-exempt in the pin (one versioned
// snapshot), so the client sends no limit/cursor there.
package sources

import (
	"testing"

	"github.com/IQVO/product-discovery/internal/application/readmodel"
)

func TestProductContentPinnedSpecCoversEveryCallSite(t *testing.T) {
	doc := loadPinnedSpec(t, specProductContent)
	assertSpecCoversCallSites(t, doc, []callSite{
		{method: "GET", path: pathProductContents, opID: "listProductContents", paged: true},
		{method: "GET", path: pathCategories, opID: "listCategories", paged: false},
	})
}

func TestProductContentExamplesSatisfyPinnedSpec(t *testing.T) {
	doc := loadPinnedSpec(t, specProductContent)
	assertSatisfiesSchema(t, "product-content.product-content-page.example.json",
		operationSchema(t, doc, pathProductContents, 200, "application/json"),
		fixture(t, "product-content.product-content-page.example.json"))
	assertSatisfiesSchema(t, "product-content.category-list.example.json",
		operationSchema(t, doc, pathCategories, 200, "application/json"),
		fixture(t, "product-content.category-list.example.json"))
	assertSatisfiesSchema(t, "product-content.problem.example.json",
		pinnedSchema(t, doc, "Problem"), fixture(t, "product-content.problem.example.json"))
}

func TestProductContentGatewayDecodesProductContentsExample(t *testing.T) {
	doc := loadPinnedSpec(t, specProductContent)
	up := &upstream{}
	cl := serveBody(t, 200, "application/json",
		string(fixture(t, "product-content.product-content-page.example.json")), up)
	page, err := (ProductContent{Client: cl}).ListProductContents(t.Context(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if up.path != pathProductContents || up.query.Get("limit") != "10" || up.query.Has("cursor") {
		t.Errorf("client called %s?%s, expected %s with limit only", up.path, up.query.Encode(), pathProductContents)
	}
	assertDecodedContentRows(t, page.Items)
	if page.Next != "" {
		t.Errorf("absent nextCursor must decode as empty, got %q", page.Next)
	}
	assertDecodes(t, doc, pathProductContents)
}

// assertDecodedContentRows checks the two fixture rows (PUBLISHED with a full
// body, DRAFT with empty collections) against the rebuild's row mapping.
func assertDecodedContentRows(t *testing.T, items []readmodel.ContentRow) {
	t.Helper()
	if len(items) != 2 {
		t.Fatalf("decoded %d content rows, expected 2", len(items))
	}
	published, draft := items[0], items[1]
	if published.CatalogItemID != "ci-4901" || published.State != "PUBLISHED" || published.Published.Revision != 4 ||
		published.Published.CategoryID != "cat-drinkware" || len(published.Published.Bullets) != 2 ||
		published.Published.Attributes[0].Unit != "ml" || published.Published.Media[0].URL == "" {
		t.Errorf("published row decoded wrong: %+v", published)
	}
	if draft.State != "DRAFT" || draft.Published.CategoryID != "" || draft.Published.Revision != 1 {
		// primaryCategoryId is omitted when unset; the DTO keeps "" — the
		// documented zero value for an unset category.
		t.Errorf("draft row (no primaryCategoryId, no media) decoded wrong: %+v", draft)
	}
}

func TestProductContentGatewayDecodesCategoriesExample(t *testing.T) {
	up := &upstream{}
	cl := serveBody(t, 200, "application/json",
		string(fixture(t, "product-content.category-list.example.json")), up)
	tree, err := (ProductContent{Client: cl}).ListCategories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if up.path != pathCategories || len(up.query) != 0 {
		t.Errorf("client called %s?%s, expected %s with no query", up.path, up.query.Encode(), pathCategories)
	}
	if tree.Version != 12 || len(tree.Categories) != 2 {
		t.Fatalf("tree decoded wrong: version %d, %d categories", tree.Version, len(tree.Categories))
	}
	root, child := tree.Categories[0], tree.Categories[1]
	if root.ID != "cat-root" || root.ParentID != "" {
		// parentId is optional ("omitted for a root category"); "" is the
		// documented zero value.
		t.Errorf("root category decoded wrong: %+v", root)
	}
	if child.ParentID != "cat-root" || child.Path != "Store/Drinkware" {
		t.Errorf("child category decoded wrong: %+v", child)
	}
}

func TestProductContentFieldsWeReadAreSpecified(t *testing.T) {
	doc := loadPinnedSpec(t, specProductContent)
	var row contentRow
	contentOptional := fieldsSpecified(t, "contentRow", pinnedSchema(t, doc, "ProductContent"), rowType(row))
	if _, ok := contentOptional["primaryCategoryId"]; !ok {
		t.Errorf("primaryCategoryId must stay optional in the pin: it is omitted when unset")
	}
	attributeOptional := fieldsSpecified(t, "contentRow.attributes", pinnedSchema(t, doc, "Attribute"),
		elemType(t, row.Attributes))
	if _, ok := attributeOptional["unit"]; !ok {
		t.Errorf("unit must stay optional in the pin: a response omits it when there is none")
	}
	fieldsSpecified(t, "contentRow.media", pinnedSchema(t, doc, "Media"), elemType(t, row.Media))
	var list categoryList
	categoryOptional := fieldsSpecified(t, "categoryList.categories", pinnedSchema(t, doc, "Category"),
		elemType(t, list.Categories))
	if _, ok := categoryOptional["parentId"]; !ok {
		t.Errorf("parentId must stay optional in the pin: a root category omits it")
	}
	fieldsSpecified(t, "categoryList", pinnedSchema(t, doc, "CategoryList"), rowType(list))
	// Absence tolerance: required-only bodies must decode into every DTO we use.
	toleratesAbsence(t, "contentRow", pinnedSchema(t, doc, "ProductContent"), &contentRow{})
	toleratesAbsence(t, "contentRow.attributes", pinnedSchema(t, doc, "Attribute"), &struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Unit  string `json:"unit"`
	}{})
	toleratesAbsence(t, "contentRow.media", pinnedSchema(t, doc, "Media"), &struct {
		MediaID string `json:"mediaId"`
		URL     string `json:"url"`
		AltText string `json:"altText"`
		Role    string `json:"role"`
	}{})
	toleratesAbsence(t, "categoryList", pinnedSchema(t, doc, "CategoryList"), &categoryList{})
	toleratesAbsence(t, "page[contentRow]", pinnedSchema(t, doc, "ProductContentPage"), &page[contentRow]{})
}
