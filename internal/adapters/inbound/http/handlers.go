package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/product-discovery/internal/adapters/inbound/http/page"
	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// maxIDLength bounds a path identifier; a longer one cannot name a resource.
const maxIDLength = 128

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}

func (s *Server) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "catalogItemId")
	if id == "" || len(id) > maxIDLength {
		writeError(w, r, ports.ErrNotFound)
		return
	}
	p, err := s.Queries.GetProduct(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, productDTO(p))
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	l, err := s.Queries.ListCategories(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoriesDTO(l))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if name := page.Unknown(r, searchFilters...); name != "" {
		writeProblem(w, r, unknownQueryParameter, "query parameter "+name+" is not declared by this operation")
		return
	}
	in, err := searchInput(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	res, err := s.Queries.SearchProducts(r.Context(), in)
	if err != nil {
		if errors.Is(err, search.ErrInvalidCursor) {
			err = page.ErrInvalidCursor
		}
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, searchDTO(res))
}

// searchFilters are the query parameters GET /products declares besides limit and cursor.
var searchFilters = []string{"q", "categoryId", "inStock", "priceMin", "priceMax", "sort"}

// searchInput reads the query string of GET /products. limit and cursor follow
// the fleet paging contract (400 problems); a filter of the wrong type is the
// same 422 validation-failed as an out-of-range one.
func searchInput(r *http.Request) (search.Input, error) {
	q := r.URL.Query()
	limit, err := page.Limit(r)
	if err != nil {
		return search.Input{}, err
	}
	cursors := q["cursor"]
	if len(cursors) > 1 {
		return search.Input{}, page.ErrInvalidCursor
	}
	in := search.Input{Text: q.Get("q"), CategoryID: q.Get("categoryId"), Sort: q.Get("sort"), Limit: limit}
	if len(cursors) == 1 {
		in.After = cursors[0]
	}
	if in.InStock, err = optBool(q.Get("inStock"), "inStock"); err != nil {
		return in, err
	}
	if in.PriceMin, err = optInt(q.Get("priceMin"), "priceMin"); err != nil {
		return in, err
	}
	if in.PriceMax, err = optInt(q.Get("priceMax"), "priceMax"); err != nil {
		return in, err
	}
	return in, nil
}

func optBool(raw, field string) (*bool, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil || (raw != "true" && raw != "false") {
		return nil, &search.ValidationError{Field: field, Reason: "must be true or false"}
	}
	return &b, nil
}

func optInt(raw, field string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, &search.ValidationError{Field: field, Reason: "must be an integer"}
	}
	return &n, nil
}
