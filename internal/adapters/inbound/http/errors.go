package http

import (
	"errors"
	"net/http"

	"github.com/IQVO/product-discovery/internal/adapters/inbound/http/page"
	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// problemBase is the namespace of this service's RFC 9457 `type` URIs.
const problemBase = "/problems/"

// errMalformedRequest marks a request the adapter itself rejects before any use
// case runs (a NUL character).
var errMalformedRequest = errors.New("request is not valid for this operation")

// problem is the fixed (status, slug, title) of one error category; the detail
// comes from the error at write time.
type problem struct {
	status int
	slug   string
	title  string
}

var internalProblem = problem{http.StatusInternalServerError, "internal-error", "Internal server error"}

var (
	invalidLimit          = problem{http.StatusBadRequest, "invalid-limit", "Invalid limit"}
	invalidCursor         = problem{http.StatusBadRequest, "invalid-cursor", "Invalid cursor"}
	unknownQueryParameter = problem{http.StatusBadRequest, "unknown-query-parameter", "Unknown query parameter"}
)

var validationFailed = problem{http.StatusUnprocessableEntity, "validation-failed", "Validation failed"}

// problemCatalogue maps every typed error to its problem, in match order. This
// read-only surface knows only not-found and validation-failed (apis/openapi.yaml).
var problemCatalogue = []struct {
	err error
	p   problem
}{
	{errMalformedRequest, problem{http.StatusBadRequest, "malformed-request", "Malformed request"}},
	{page.ErrInvalidLimit, invalidLimit},
	{page.ErrInvalidCursor, invalidCursor},
	{search.ErrInvalidCursor, invalidCursor},
	{ports.ErrNotFound, problem{http.StatusNotFound, "not-found", "Not found"}},
	{search.ErrValidation, validationFailed},
}

// problemFor returns the problem of the first catalogue entry err matches, or
// the internal-error problem.
func problemFor(err error) problem {
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry.p
		}
	}
	return internalProblem
}
