package bdd

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

func (w *world) product(id string) (map[string]any, error) {
	if err := w.get("/products/" + id); err != nil {
		return nil, err
	}
	if w.status != 200 {
		return nil, fmt.Errorf("GET /products/%s = %d %s", id, w.status, w.body)
	}
	return w.json, nil
}

func (w *world) flag(id, field string, want bool) error {
	p, err := w.product(id)
	if err != nil {
		return err
	}
	if p[field] != want {
		return fmt.Errorf("%s of %s = %v, want %v", field, id, p[field], want)
	}
	return nil
}

func splitIDs(list string) []string {
	var out []string
	for _, s := range strings.Split(list, ",") {
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

func registerAssertions(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the product "([^"]*)" is purchasable and in stock$`, func(id string) error {
		if err := w.flag(id, "purchasable", true); err != nil {
			return err
		}
		return w.flag(id, "inStock", true)
	})
	sc.Step(`^the product "([^"]*)" is not purchasable$`, func(id string) error { return w.flag(id, "purchasable", false) })
	sc.Step(`^the product "([^"]*)" is not in stock$`, func(id string) error { return w.flag(id, "inStock", false) })
	sc.Step(`^the product "([^"]*)" is not found$`, func(id string) error {
		if err := w.get("/products/" + id); err != nil || w.status != 404 {
			return fmt.Errorf("status %d, err %v: a stub must be invisible", w.status, err)
		}
		return nil
	})
	registerProductFields(sc, w)
}

func registerProductFields(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the product "([^"]*)" has the title "([^"]*)"$`, func(id, title string) error {
		p, err := w.product(id)
		if err != nil || p["title"] != title {
			return fmt.Errorf("title = %v, %v", p["title"], err)
		}
		return nil
	})
	sc.Step(`^the product "([^"]*)" has effective price (\d+)$`, func(id string, price int) error {
		p, err := w.product(id)
		if err != nil {
			return err
		}
		eff, _ := p["effectivePrice"].(map[string]any)
		if eff["amountMinor"] != float64(price) {
			return fmt.Errorf("effectivePrice = %v, want %d", eff, price)
		}
		return nil
	})
	sc.Step(`^the product "([^"]*)" has the category path "([^"]*)"$`, func(id, path string) error {
		p, err := w.product(id)
		if err != nil {
			return err
		}
		var names []string
		for _, n := range p["categoryPath"].([]any) {
			names = append(names, n.(string))
		}
		if got := strings.Join(names, " > "); got != path {
			return fmt.Errorf("categoryPath = %q, want %q", got, path)
		}
		return nil
	})
	sc.Step(`^the search for "([^"]*)" finds nothing$`, func(text string) error {
		if err := w.get("/products?q=" + text); err != nil {
			return err
		}
		if ids := w.hitIDs(); len(ids) != 0 {
			return fmt.Errorf("hits = %v", ids)
		}
		return nil
	})
}
