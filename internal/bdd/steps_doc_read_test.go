package bdd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

// steps_doc_read_test.go — read-side vocabulary of the documentation-derived
// scenarios: raw requests with headers, JSON path assertions, search page and
// facet assertions, and the ETag steps.

// request sends one raw request and records the answer, its body and headers.
func (w *world) request(method, path string, header map[string]string) error {
	req, err := http.NewRequest(method, w.srv.URL+path, nil)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	w.status, w.body, w.json, w.doc.header = resp.StatusCode, string(b), nil, resp.Header
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		w.json = m
	}
	return nil
}

// lookup follows a dotted path ("attributes.0.unit") through the last answer.
func lookup(root any, path string) (any, bool) {
	cur := root
	for _, part := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func (w *world) field(path string) (any, error) {
	v, ok := lookup(w.json, path)
	if !ok {
		return nil, fmt.Errorf("no field %q in %s", path, strings.TrimSpace(w.body))
	}
	return v, nil
}

func registerDocRead(sc *godog.ScenarioContext, w *world) {
	registerDocFields(sc, w)
	registerDocHits(sc, w)
	registerDocFacets(sc, w)
}

func registerDocFields(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the response field "([^"]*)" is "([^"]*)"$`, func(path, want string) error {
		got, err := w.field(path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %v, want %q", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response field "([^"]*)" is (-?\d+)$`, func(path string, want int) error {
		got, err := w.field(path)
		if err != nil {
			return err
		}
		if got != float64(want) {
			return fmt.Errorf("%s = %v, want %d", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response field "([^"]*)" is (true|false)$`, func(path, want string) error {
		got, err := w.field(path)
		if err != nil {
			return err
		}
		if got != (want == "true") {
			return fmt.Errorf("%s = %v, want %s", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response has no field "([^"]*)"$`, func(path string) error {
		if v, ok := lookup(w.json, path); ok {
			return fmt.Errorf("field %q = %v, want it absent", path, v)
		}
		return nil
	})
	sc.Step(`^the response list "([^"]*)" has (\d+) items?$`, func(path string, n int) error {
		got, err := w.field(path)
		if err != nil {
			return err
		}
		list, _ := got.([]any)
		if len(list) != n {
			return fmt.Errorf("%s has %d items, want %d", path, len(list), n)
		}
		return nil
	})
	sc.Step(`^the shopper requests "([^"]*)"$`, func(path string) error { return w.request(http.MethodGet, path, nil) })
	sc.Step(`^the shopper searches for a text of (\d+) characters$`, func(n int) error {
		return w.search(url.Values{"q": {strings.Repeat("k", n)}})
	})
	registerDocExtras(sc, w)
}

// registerDocExtras registers the small assertions the rule scenarios share.
func registerDocExtras(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the response field "([^"]*)" is null$`, func(path string) error {
		got, err := w.field(path)
		if err != nil {
			return err
		}
		if got != nil {
			return fmt.Errorf("%s = %v, want null", path, got)
		}
		return nil
	})
	sc.Step(`^the product "([^"]*)" shows (no price|the effective price (\d+))$`, func(id, what, amount string) error {
		p, err := w.product(id)
		if err != nil {
			return err
		}
		eff, priced := p["effectivePrice"].(map[string]any)
		switch {
		case amount == "" && priced:
			return fmt.Errorf("product %s shows the price %v, want none", id, eff)
		case amount != "" && (!priced || fmt.Sprint(eff["amountMinor"]) != amount):
			return fmt.Errorf("product %s shows %v, want the effective price %s", id, p["effectivePrice"], amount)
		}
		return nil
	})
	sc.Step(`^the first category suggestion is "([^"]*)"$`, func(id string) error {
		sugg, _ := w.json["categorySuggestions"].([]any)
		if len(sugg) == 0 || sugg[0].(map[string]any)["categoryId"] != id {
			return fmt.Errorf("suggestions = %v, want %s first", sugg, id)
		}
		return nil
	})
	sc.Step(`^the hit "([^"]*)" shows the primary image "([^"]*)"$`, func(id, media string) error {
		h, err := w.hit(id)
		if err != nil {
			return err
		}
		if m, _ := h["primaryMedia"].(map[string]any); m["mediaId"] != media {
			return fmt.Errorf("hit %s primaryMedia = %v, want %s", id, h["primaryMedia"], media)
		}
		return nil
	})
	sc.Step(`^the answer is served as problem\+json$`, func() error {
		if got := w.doc.header.Get("Content-Type"); got != "application/problem+json" {
			return fmt.Errorf("Content-Type = %q", got)
		}
		return nil
	})
}

// hit finds one hit of the last search page by its catalogue item id.
func (w *world) hit(id string) (map[string]any, error) {
	items, _ := w.json["items"].([]any)
	for _, it := range items {
		if m := it.(map[string]any); m["catalogItemId"] == id {
			return m, nil
		}
	}
	return nil, fmt.Errorf("%s is not among the hits %v", id, w.hitIDs())
}

func registerDocHits(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^there are no hits$`, func() error {
		if ids := w.hitIDs(); len(ids) != 0 {
			return fmt.Errorf("hits = %v, want none", ids)
		}
		return nil
	})
	sc.Step(`^there is (a|no) next page$`, func(which string) error {
		_, has := w.json["nextCursor"]
		if has != (which == "a") {
			return fmt.Errorf("nextCursor present = %v in %s", has, strings.TrimSpace(w.body))
		}
		return nil
	})
	sc.Step(`^the hit "([^"]*)" is (purchasable|unpurchasable) and (in stock|out of stock)$`, func(id, buy, stock string) error {
		h, err := w.hit(id)
		if err != nil {
			return err
		}
		if h["purchasable"] != (buy == "purchasable") || h["inStock"] != (stock == "in stock") {
			return fmt.Errorf("hit %s = %v", id, h)
		}
		return nil
	})
	sc.Step(`^the hit "([^"]*)" has the category path "([^"]*)"$`, func(id, path string) error {
		h, err := w.hit(id)
		if err != nil {
			return err
		}
		var names []string
		for _, n := range h["categoryPath"].([]any) {
			names = append(names, n.(string))
		}
		if got := strings.Join(names, " > "); got != path {
			return fmt.Errorf("hit %s categoryPath = %q, want %q", id, got, path)
		}
		return nil
	})
	sc.Step(`^the hit "([^"]*)" shows no price$`, func(id string) error {
		h, err := w.hit(id)
		if err != nil {
			return err
		}
		if _, priced := h["effectivePrice"]; priced {
			return fmt.Errorf("hit %s shows a price: %v", id, h)
		}
		return nil
	})
	sc.Step(`^the product "([^"]*)" is reported (purchasable|unpurchasable) and (in stock|out of stock)$`, func(id, buy, stock string) error {
		p, err := w.product(id)
		if err != nil {
			return err
		}
		if p["purchasable"] != (buy == "purchasable") || p["inStock"] != (stock == "in stock") {
			return fmt.Errorf("product %s = purchasable %v, inStock %v", id, p["purchasable"], p["inStock"])
		}
		return nil
	})
	sc.Step(`^there are (\d+) category suggestions$`, func(n int) error {
		sugg, _ := w.json["categorySuggestions"].([]any)
		if len(sugg) != n {
			return fmt.Errorf("suggestions = %d, want %d", len(sugg), n)
		}
		return nil
	})
}

func registerDocFacets(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the facet "([^"]*)" has "([^"]*)" with count (\d+)$`, func(name, value string, n int) error {
		counts, err := w.facet(name)
		if err != nil {
			return err
		}
		if counts[value] != float64(n) {
			return fmt.Errorf("facet %s = %v, want %s=%d", name, counts, value, n)
		}
		return nil
	})
	sc.Step(`^the facet "([^"]*)" has no "([^"]*)" bucket$`, func(name, value string) error {
		counts, err := w.facet(name)
		if err != nil {
			return err
		}
		if _, ok := counts[value]; ok {
			return fmt.Errorf("facet %s = %v, want no %s bucket", name, counts, value)
		}
		return nil
	})
	sc.Step(`^the facets are listed in the order "([^"]*)"$`, func(order string) error {
		var names []string
		facets, _ := w.json["facets"].([]any)
		for _, f := range facets {
			names = append(names, f.(map[string]any)["name"].(string))
		}
		if got := strings.Join(names, ", "); got != order {
			return fmt.Errorf("facets = %s, want %s", got, order)
		}
		return nil
	})
}

// registerDocHTTP registers the steps about verbs, cursors and cache validators.
func registerDocHTTP(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a (POST|PUT|PATCH|DELETE) request is sent to "([^"]*)"$`, func(method, path string) error {
		return w.request(method, path, nil)
	})
	sc.Step(`^the shopper reuses the next cursor on "([^"]*)"$`, func(path string) error {
		cursor, ok := w.json["nextCursor"].(string)
		if !ok {
			return fmt.Errorf("the last page has no next cursor: %s", strings.TrimSpace(w.body))
		}
		return w.get(path + "&cursor=" + url.QueryEscape(cursor))
	})
	sc.Step(`^the shopper pages through "([^"]*)" (\d+) at a time$`, func(base string, n int) error {
		w.pages = nil
		cursor := ""
		for i := 0; i < 20; i++ {
			path := fmt.Sprintf("/products?%s&limit=%d", base, n)
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			if err := w.get(path); err != nil {
				return err
			}
			w.pages = append(w.pages, w.hitIDs()...)
			next, ok := w.json["nextCursor"].(string)
			if !ok {
				return nil
			}
			cursor = next
		}
		return fmt.Errorf("the cursor never ended")
	})
	registerDocETag(sc, w)
}

func registerDocETag(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the shopper reads "([^"]*)" noting its ETag$`, func(path string) error {
		if err := w.request(http.MethodGet, path, nil); err != nil {
			return err
		}
		w.doc.etag = w.doc.header.Get("ETag")
		return nil
	})
	sc.Step(`^the shopper reads "([^"]*)" presenting (the noted ETag|a weak copy of the noted ETag|the wildcard ETag|an unrelated ETag)$`,
		func(path, which string) error {
			presented := map[string]string{
				"the noted ETag":                w.doc.etag,
				"a weak copy of the noted ETag": "W/" + w.doc.etag,
				"the wildcard ETag":             "*",
				"an unrelated ETag":             `"0000000000000000"`,
			}[which]
			return w.request(http.MethodGet, path, map[string]string{"If-None-Match": presented})
		})
	sc.Step(`^the answer carries an ETag and may be cached for (\d+) seconds$`, func(seconds int) error {
		if w.doc.header.Get("ETag") == "" {
			return fmt.Errorf("no ETag header")
		}
		if got, want := w.doc.header.Get("Cache-Control"), fmt.Sprintf("max-age=%d", seconds); got != want {
			return fmt.Errorf("Cache-Control = %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the answer has no body$`, func() error {
		if w.body != "" {
			return fmt.Errorf("body = %q, want none", w.body)
		}
		return nil
	})
	sc.Step(`^the ETag (differs from|equals) the noted one$`, func(which string) error {
		got := w.doc.header.Get("ETag")
		if got == "" || (got == w.doc.etag) != (which == "equals") {
			return fmt.Errorf("ETag = %q, noted %q", got, w.doc.etag)
		}
		return nil
	})
}
