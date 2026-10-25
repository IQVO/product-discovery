package http_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestGetProductReturnsTheFullView(t *testing.T) {
	e := newEnv(t)
	e.tree()
	e.item("I1", "Steel Kettle", 3000)
	r := e.get("/products/I1")
	e.wantStatus(r, http.StatusOK)
	m := r.json(t)
	if m["catalogItemId"] != "I1" || m["title"] != "Steel Kettle" || m["hazmat"] != true || m["fragile"] != false ||
		m["inStock"] != true || m["purchasable"] != true || m["description"] != "Boils water" || m["categoryId"] != "kettles" {
		t.Fatalf("view = %s", r.Body)
	}
	if p := m["categoryPath"].([]any); len(p) != 2 || p[0] != "Home" || p[1] != "Kettles" {
		t.Fatalf("categoryPath = %v", p)
	}
}

func TestGetProductShowsPricesAttributesAndMedia(t *testing.T) {
	e := newEnv(t)
	e.item("I1", "Steel Kettle", 3000)
	m := e.get("/products/I1").json(t)
	eff := m["effectivePrice"].(map[string]any)
	list := m["listPrice"].(map[string]any)
	if eff["amountMinor"] != float64(3000) || eff["currency"] != "USD" || list["amountMinor"] != float64(3500) {
		t.Fatalf("prices = %v %v", eff, list)
	}
	attrs := m["attributes"].([]any)
	if attrs[0].(map[string]any)["unit"] != "l" || attrs[1].(map[string]any)["unit"] != nil {
		t.Fatalf("attributes = %v", attrs)
	}
	if media := m["media"].([]any)[0].(map[string]any); media["role"] != "PRIMARY" || media["mediaId"] != "m1" {
		t.Fatalf("media = %v", media)
	}
}

func TestGetProductHidesStubsAndLongIDs(t *testing.T) {
	e := newEnv(t)
	e.wantProblem(e.get("/products/nope"), http.StatusNotFound, "not-found")
	e.wantProblem(e.get("/products/"+strings.Repeat("a", 200)), http.StatusNotFound, "not-found")
}

func TestGetProductOmitsWithdrawnPriceAndRetiredContent(t *testing.T) {
	e := newEnv(t)
	e.item("I1", "Kettle", 100)
	e.retireAndWithdraw("I1")
	m := e.get("/products/I1").json(t)
	if _, ok := m["effectivePrice"]; ok {
		t.Fatalf("a withdrawn price is not shown: %v", m)
	}
	if _, ok := m["description"]; ok || m["purchasable"] != false {
		t.Fatalf("retired content is not shown: %v", m)
	}
}

func TestListCategoriesCountsPurchasable(t *testing.T) {
	e := newEnv(t)
	r := e.get("/categories")
	e.wantStatus(r, http.StatusOK)
	if m := r.json(t); m["version"] != float64(0) || len(m["categories"].([]any)) != 0 {
		t.Fatalf("empty = %s", r.Body)
	}
	e.tree()
	e.item("I1", "Kettle", 100)
	cats := e.get("/categories").json(t)["categories"].([]any)
	home, kettles := cats[0].(map[string]any), cats[1].(map[string]any)
	if home["categoryId"] != "home" || home["parentId"] != nil || home["purchasableCount"] != float64(1) ||
		kettles["parentId"] != "home" || kettles["purchasableCount"] != float64(1) || kettles["path"] != "Home/Kettles" {
		t.Fatalf("categories = %v", cats)
	}
}
