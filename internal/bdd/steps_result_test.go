package bdd

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/cucumber/godog"
)

func (w *world) facet(name string) (map[string]float64, error) {
	facets, _ := w.json["facets"].([]any)
	for _, f := range facets {
		fm := f.(map[string]any)
		if fm["name"] != name {
			continue
		}
		out := map[string]float64{}
		for _, v := range fm["values"].([]any) {
			vm := v.(map[string]any)
			out[vm["value"].(string)] = vm["count"].(float64)
		}
		return out, nil
	}
	return nil, fmt.Errorf("no facet %q in %s", name, w.body)
}

func registerResults(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the hits are "([^"]*)" in any order$`, func(list string) error {
		got, want := w.hitIDs(), splitIDs(list)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("hits = %v, want %v", got, want)
		}
		return nil
	})
	sc.Step(`^the hits are exactly "([^"]*)"$`, func(list string) error {
		if got := join(w.hitIDs()); got != join(splitIDs(list)) {
			return fmt.Errorf("hits = %s, want %s", got, list)
		}
		return nil
	})
	sc.Step(`^the category suggestions are "([^"]*)"$`, func(list string) error {
		var got []string
		sugg, _ := w.json["categorySuggestions"].([]any)
		for _, s := range sugg {
			got = append(got, s.(map[string]any)["categoryId"].(string))
		}
		if join(got) != join(splitIDs(list)) {
			return fmt.Errorf("suggestions = %v, want %s", got, list)
		}
		return nil
	})
	sc.Step(`^there are (\d+) hit-independent facets$`, func(n int) error {
		if got := len(w.json["facets"].([]any)); got != n {
			return fmt.Errorf("facets = %d, want %d", got, n)
		}
		return nil
	})
	sc.Step(`^the price-band facet counts "([^"]*)" as (\d+) and "([^"]*)" as (\d+)$`, func(a string, na int, b string, nb int) error {
		counts, err := w.facet("price-band")
		if err != nil {
			return err
		}
		if counts[a] != float64(na) || counts[b] != float64(nb) {
			return fmt.Errorf("price-band = %v, want %s=%d %s=%d", counts, a, na, b, nb)
		}
		return nil
	})
	sc.Step(`^the pages concatenate to "([^"]*)" in order$`, func(list string) error {
		if got := join(w.pages); got != join(splitIDs(list)) {
			return fmt.Errorf("pages = %s, want %s", got, list)
		}
		return nil
	})
	sc.Step(`^the answer is (\d+) "([^"]*)"$`, func(status int, slug string) error {
		if w.status != status || w.json["type"] != "/problems/"+slug {
			return fmt.Errorf("answer = %d %s", w.status, strings.TrimSpace(w.body))
		}
		return nil
	})
}
