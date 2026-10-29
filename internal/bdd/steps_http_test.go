package bdd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
)

// get records the last API answer.
func (w *world) get(path string) error {
	resp, err := http.Get(w.srv.URL + path)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	w.status, w.body, w.json = resp.StatusCode, string(b), nil
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		w.json = m
	}
	return nil
}

func (w *world) hitIDs() []string {
	var ids []string
	hits, _ := w.json["hits"].([]any)
	for _, h := range hits {
		ids = append(ids, h.(map[string]any)["catalogItemId"].(string))
	}
	return ids
}

func (w *world) search(q url.Values) error { return w.get("/products?" + q.Encode()) }

func registerSearch(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the shopper searches for "([^"]*)"$`, func(text string) error {
		return w.search(url.Values{"q": {text}})
	})
	sc.Step(`^the shopper searches with price from (\d+) to (\d+) sorted by "([^"]*)"$`, func(lo, hi int, sortBy string) error {
		return w.search(url.Values{"priceMin": {fmt.Sprint(lo)}, "priceMax": {fmt.Sprint(hi)}, "sort": {sortBy}})
	})
	sc.Step(`^the shopper searches inside category "([^"]*)" sorted by "([^"]*)"$`, func(category, sortBy string) error {
		return w.search(url.Values{"categoryId": {category}, "sort": {sortBy}})
	})
	sc.Step(`^the shopper searches with page size (\d+)$`, func(n int) error {
		return w.search(url.Values{"limit": {fmt.Sprint(n)}})
	})
	sc.Step(`^the shopper searches with sort "([^"]*)"$`, func(sortBy string) error {
		return w.search(url.Values{"sort": {sortBy}})
	})
	sc.Step(`^the shopper pages through everything sorted by "([^"]*)" (\d+) at a time$`, func(sortBy string, n int) error {
		w.pages = nil
		after := ""
		for i := 0; i < 20; i++ {
			q := url.Values{"sort": {sortBy}, "limit": {fmt.Sprint(n)}}
			if after != "" {
				q.Set("after", after)
			}
			if err := w.search(q); err != nil {
				return err
			}
			w.pages = append(w.pages, w.hitIDs()...)
			next, ok := w.json["next"].(string)
			if !ok {
				return nil
			}
			after = next
		}
		return fmt.Errorf("the cursor never ended")
	})
	sc.Step(`^the shopper opens the product "([^"]*)"$`, func(id string) error { return w.get("/products/" + id) })
}

func join(ids []string) string { return strings.Join(ids, ", ") }
