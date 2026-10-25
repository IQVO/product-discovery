package bdd

import (
	"fmt"

	"github.com/cucumber/godog"
)

func (w *world) categories() (version float64, byID map[string]map[string]any, err error) {
	if err = w.get("/categories"); err != nil {
		return 0, nil, err
	}
	byID = map[string]map[string]any{}
	cats, _ := w.json["categories"].([]any)
	for _, c := range cats {
		cm := c.(map[string]any)
		byID[cm["categoryId"].(string)] = cm
	}
	version, _ = w.json["version"].(float64)
	return version, byID, nil
}

func registerCategories(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the category "([^"]*)" has (\d+) purchasable products$`, func(id string, n int) error {
		_, cats, err := w.categories()
		if err != nil {
			return err
		}
		if cats[id]["purchasableCount"] != float64(n) {
			return fmt.Errorf("category %s = %v, want %d", id, cats[id], n)
		}
		return nil
	})
	sc.Step(`^the product catalogue still shows the category "([^"]*)" named "([^"]*)"$`, func(id, name string) error {
		_, cats, err := w.categories()
		if err != nil {
			return err
		}
		if cats[id]["name"] != name {
			return fmt.Errorf("category %s = %v, want name %s", id, cats[id], name)
		}
		return nil
	})
	sc.Step(`^the category list is empty at version (\d+)$`, func(version int) error {
		v, cats, err := w.categories()
		if err != nil {
			return err
		}
		if v != float64(version) || len(cats) != 0 {
			return fmt.Errorf("version %v with %d categories", v, len(cats))
		}
		return nil
	})
}
