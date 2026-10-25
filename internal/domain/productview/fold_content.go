package productview

import "github.com/IQVO/product-discovery/internal/domain/shared"

func validCategoryID(id string) bool { return shared.ValidID(id) }

// PublishedContent is ProductContentPublished (with the bullets and media of
// decision H2).
type PublishedContent struct {
	Revision    int64
	Description string
	Bullets     []string
	Attributes  []Attribute
	Media       []Media
	CategoryID  string
}

// ApplyContentPublished replaces the content when the revision is strictly
// newer than the one folded so far (the content-revision guard). A published
// revision also revives content that a retire of an older revision ended.
func (v View) ApplyContentPublished(f PublishedContent) (View, Outcome, error) {
	if f.Revision < 1 {
		return v, Stale, invalid("content of %s: revision %d", v.CatalogItemID, f.Revision)
	}
	if f.CategoryID != "" && !validCategoryID(f.CategoryID) {
		return v, Stale, invalid("content of %s: category id %q", v.CatalogItemID, f.CategoryID)
	}
	if f.Revision <= v.ContentRevision {
		return v, Stale, nil
	}
	v.Content, v.ContentRevision = ContentPublished, f.Revision
	v.Description = f.Description
	v.Bullets = append([]string(nil), f.Bullets...)
	v.Attributes = append([]Attribute(nil), f.Attributes...)
	v.Media = append([]Media(nil), f.Media...)
	v.CategoryID = f.CategoryID
	return v, Applied, nil
}

// ApplyContentRetired ends the published content. revision 0 means the fact
// did not say which revision it retires and is always applied; a retire of a
// revision older than the one folded is stale.
func (v View) ApplyContentRetired(revision int64) (View, Outcome, error) {
	if revision < 0 {
		return v, Stale, invalid("content of %s: revision %d", v.CatalogItemID, revision)
	}
	if v.Content == ContentRetired || (revision != 0 && revision < v.ContentRevision) {
		return v, Stale, nil
	}
	v.Content = ContentRetired
	if revision > v.ContentRevision {
		v.ContentRevision = revision
	}
	return v, Applied, nil
}
