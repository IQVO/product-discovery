// Package categoryview holds CategoryView, the browsable category tree the
// storefront serves: one published version of the whole tree, replaced
// wholesale by CategoryTreePublished and guarded by its version. Purchasable
// counts are not stored here; they are derived from ProductView and rolled up
// the tree by Counted. It is pure: no I/O, no clock.
package categoryview

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/IQVO/product-discovery/internal/domain/search"
	"github.com/IQVO/product-discovery/internal/domain/shared"
)

// Category is one node of the published tree.
type Category struct {
	ID       string
	ParentID string // empty for a root
	Name     string
	Path     string
}

// Counted is a category with its purchasable count, descendants included.
type Counted struct {
	Category
	PurchasableCount int
}

// View is one published version of the tree. The zero View is "no tree
// published yet": version 0, no categories.
type View struct {
	version int64
	cats    []Category
	byID    map[string]int
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: category tree: %s", shared.ErrInvalidFact, fmt.Sprintf(format, a...))
}

// New builds the view of a published tree. Ids must be valid and unique, names
// non-empty, every parent present, and the parent links acyclic.
func New(version int64, cats []Category) (View, error) {
	if version < 1 {
		return View{}, invalid("version %d must be at least 1", version)
	}
	v := View{version: version, byID: make(map[string]int, len(cats))}
	for _, c := range cats {
		if !shared.ValidID(c.ID) {
			return View{}, invalid("category id %q", c.ID)
		}
		if c.Name == "" {
			return View{}, invalid("category %s has no name", c.ID)
		}
		if _, dup := v.byID[c.ID]; dup {
			return View{}, invalid("duplicate category %s", c.ID)
		}
		v.byID[c.ID] = len(v.cats)
		v.cats = append(v.cats, c)
	}
	slices.SortStableFunc(v.cats, func(a, b Category) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.ID, b.ID))
	})
	for i, c := range v.cats {
		v.byID[c.ID] = i
	}
	if err := v.checkParents(); err != nil {
		return View{}, err
	}
	return v, nil
}

func (v View) checkParents() error {
	for _, c := range v.cats {
		if c.ParentID != "" {
			if _, ok := v.byID[c.ParentID]; !ok {
				return invalid("category %s has unknown parent %s", c.ID, c.ParentID)
			}
		}
		seen := map[string]bool{}
		for cur := c; ; {
			if seen[cur.ID] {
				return invalid("category %s is in a parent cycle", c.ID)
			}
			seen[cur.ID] = true
			if cur.ParentID == "" {
				break
			}
			cur = v.cats[v.byID[cur.ParentID]]
		}
	}
	return nil
}

// Replace applies a CategoryTreePublished fact: the tree becomes the new
// version unless that version is not newer than the current one (a stale or
// duplicate publication), in which case the current view is returned and
// applied is false.
func Replace(cur View, version int64, cats []Category) (next View, applied bool, err error) {
	if version <= cur.version && version >= 1 {
		return cur, false, nil
	}
	next, err = New(version, cats)
	if err != nil {
		return cur, false, err
	}
	return next, true, nil
}

// Version is the published tree version (0 before the first publication).
func (v View) Version() int64 { return v.version }

// Categories lists the tree ordered by path then id.
func (v View) Categories() []Category { return append([]Category(nil), v.cats...) }

// Get returns the category with id.
func (v View) Get(id string) (Category, bool) {
	i, ok := v.byID[id]
	if !ok {
		return Category{}, false
	}
	return v.cats[i], true
}

// Names returns the category names from the root down to id, or nil when id is
// unknown or empty.
func (v View) Names(id string) []string {
	c, ok := v.Get(id)
	if !ok {
		return nil
	}
	var rev []string
	for {
		rev = append(rev, c.Name)
		if c.ParentID == "" {
			break
		}
		c = v.cats[v.byID[c.ParentID]]
	}
	out := make([]string, len(rev))
	for i, n := range rev {
		out[len(rev)-1-i] = n
	}
	return out
}

// Subtree returns id and every descendant id, or nil when id is unknown.
func (v View) Subtree(id string) []string {
	if _, ok := v.byID[id]; !ok {
		return nil
	}
	children := map[string][]string{}
	for _, c := range v.cats {
		children[c.ParentID] = append(children[c.ParentID], c.ID)
	}
	out := []string{id}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// Counted pairs every category with its purchasable count: direct holds the
// purchasable products per category id, and a parent counts its descendants'.
func (v View) Counted(direct map[string]int) []Counted {
	out := make([]Counted, len(v.cats))
	for i, c := range v.cats {
		n := 0
		for _, id := range v.Subtree(c.ID) {
			n += direct[id]
		}
		out[i] = Counted{Category: c, PurchasableCount: n}
	}
	return out
}

// Match returns the ids of the categories whose name covers every word of
// text: a product-type query ("kettles") names the category ("Kettle").
func (v View) Match(text string) []string {
	var ids []string
	for _, c := range v.cats {
		if search.CoversAll(c.Name, text) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// Suggestions returns up to limit categories matching text, shallowest first,
// for a product-type query.
func (v View) Suggestions(text string, limit int) []Category {
	var out []Category
	for _, id := range v.Match(text) {
		c, _ := v.Get(id)
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b Category) int {
		return cmp.Compare(len(v.Names(a.ID)), len(v.Names(b.ID)))
	})
	if limit > 0 {
		out = out[:min(limit, len(out))]
	}
	return out
}
