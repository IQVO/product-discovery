package categoryview_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/IQVO/product-discovery/internal/domain/categoryview"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

func tree() []categoryview.Category {
	return []categoryview.Category{
		{ID: "kettles", ParentID: "kitchen", Name: "Kettles", Path: "Home/Kitchen/Kettles"},
		{ID: "home", Name: "Home", Path: "Home"},
		{ID: "kitchen", ParentID: "home", Name: "Kitchen", Path: "Home/Kitchen"},
		{ID: "garden", ParentID: "home", Name: "Garden", Path: "Home/Garden"},
	}
}

func mustNew(t *testing.T) categoryview.View {
	t.Helper()
	v, err := categoryview.New(3, tree())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewSortsByPathAndExposesVersion(t *testing.T) {
	v := mustNew(t)
	var ids []string
	for _, c := range v.Categories() {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"home", "garden", "kitchen", "kettles"}) || v.Version() != 3 {
		t.Fatalf("ids = %v version %d", ids, v.Version())
	}
	if (categoryview.View{}).Version() != 0 || len((categoryview.View{}).Categories()) != 0 {
		t.Fatal("zero view is empty")
	}
}

func TestNewRejectsBadTrees(t *testing.T) {
	cases := map[string][]categoryview.Category{
		"bad id":      {{ID: "bad id", Name: "x"}},
		"no name":     {{ID: "a"}},
		"duplicate":   {{ID: "a", Name: "x"}, {ID: "a", Name: "y"}},
		"no parent":   {{ID: "a", Name: "x", ParentID: "zz"}},
		"cycle":       {{ID: "a", Name: "x", ParentID: "b"}, {ID: "b", Name: "y", ParentID: "a"}},
		"self parent": {{ID: "a", Name: "x", ParentID: "a"}},
	}
	for name, cats := range cases {
		if _, err := categoryview.New(1, cats); !errors.Is(err, shared.ErrInvalidFact) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := categoryview.New(0, nil); !errors.Is(err, shared.ErrInvalidFact) {
		t.Errorf("version 0: %v", err)
	}
	if _, err := categoryview.New(1, nil); err != nil {
		t.Errorf("an empty tree is valid: %v", err)
	}
}

func TestReplaceGuardsOnVersion(t *testing.T) {
	cur := mustNew(t)
	same, applied, err := categoryview.Replace(cur, 3, nil)
	if err != nil || applied || same.Version() != 3 || len(same.Categories()) != 4 {
		t.Fatalf("equal version: %+v %v %v", same, applied, err)
	}
	if _, applied, _ := categoryview.Replace(cur, 2, nil); applied {
		t.Fatal("older version must be stale")
	}
}

func TestReplaceAppliesNewerVersions(t *testing.T) {
	cur := mustNew(t)
	next, applied, err := categoryview.Replace(cur, 4, tree()[1:3])
	if err != nil || !applied || next.Version() != 4 || len(next.Categories()) != 2 {
		t.Fatalf("newer version: %+v %v %v", next, applied, err)
	}
	if got, applied, err := categoryview.Replace(cur, 5, []categoryview.Category{{ID: "a"}}); err == nil || applied || got.Version() != 3 {
		t.Fatalf("invalid newer tree must keep the current one: %v %v", applied, err)
	}
	if _, _, err := categoryview.Replace(cur, 0, nil); !errors.Is(err, shared.ErrInvalidFact) {
		t.Fatalf("version 0: %v", err)
	}
	first, applied, err := categoryview.Replace(categoryview.View{}, 1, tree())
	if err != nil || !applied || first.Version() != 1 {
		t.Fatalf("first publication: %v %v", applied, err)
	}
}

func TestNamesSubtreeAndGet(t *testing.T) {
	v := mustNew(t)
	if got := v.Names("kettles"); !reflect.DeepEqual(got, []string{"Home", "Kitchen", "Kettles"}) {
		t.Errorf("Names = %v", got)
	}
	if v.Names("nope") != nil || v.Names("") != nil {
		t.Error("unknown id has no names")
	}
	if got := v.Subtree("home"); len(got) != 4 || got[0] != "home" {
		t.Errorf("Subtree(home) = %v", got)
	}
	if got := v.Subtree("kettles"); !reflect.DeepEqual(got, []string{"kettles"}) {
		t.Errorf("Subtree(leaf) = %v", got)
	}
	if v.Subtree("nope") != nil {
		t.Error("unknown subtree must be nil")
	}
	if c, ok := v.Get("kitchen"); !ok || c.Name != "Kitchen" {
		t.Errorf("Get = %+v %v", c, ok)
	}
}

func TestCountedRollsUpDescendants(t *testing.T) {
	v := mustNew(t)
	got := map[string]int{}
	for _, c := range v.Counted(map[string]int{"kettles": 2, "kitchen": 1, "garden": 4}) {
		got[c.ID] = c.PurchasableCount
	}
	want := map[string]int{"home": 7, "garden": 4, "kitchen": 3, "kettles": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Counted = %v, want %v", got, want)
	}
}

func TestMatchAndSuggestions(t *testing.T) {
	v := mustNew(t)
	if got := v.Match("kettle"); !reflect.DeepEqual(got, []string{"kettles"}) {
		t.Errorf("Match = %v", got)
	}
	if v.Match("toaster") != nil || v.Match("") != nil {
		t.Error("no match expected")
	}
	cats := []categoryview.Category{
		{ID: "a", Name: "Kettles", Path: "A"},
		{ID: "b", ParentID: "a", Name: "Kettle Parts", Path: "A/B"},
		{ID: "c", ParentID: "b", Name: "Kettle Lids", Path: "A/B/C"},
	}
	w, _ := categoryview.New(1, cats)
	s := w.Suggestions("kettle", 2)
	if len(s) != 2 || s[0].ID != "a" || s[1].ID != "b" {
		t.Errorf("Suggestions = %+v", s)
	}
	if len(w.Suggestions("kettle", 0)) != 3 {
		t.Error("limit 0 means no limit")
	}
}
