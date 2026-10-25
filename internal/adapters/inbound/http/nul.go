package http

import (
	"net/http"
	"strings"
)

// notFoundResource is the 404 of a request path that can never name a resource.
var notFoundResource = problem{status: http.StatusNotFound, slug: "resource-not-found", title: "Resource not found"}

// rejectNUL turns a NUL character in a request into a client error before it
// can reach a Postgres text parameter. PostgreSQL text cannot hold U+0000, and
// a statement that carries one aborts the surrounding transaction (SQLSTATE
// 22021, then 25P02 for the next statement): on the sibling services that
// became a 500 on the real runtime, hidden for as long as the in-memory
// adapters served the contract run .
//
//   - a NUL in the URL path can never name a resource: 404 resource-not-found,
//     the same answer as any other identifier that does not exist;
//   - a NUL in the query string (raw byte or %00) is a malformed request:
//     400 malformed-request. This service has no request body.
func rejectNUL(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ContainsRune(r.URL.Path, 0) || strings.ContainsRune(r.URL.RawPath, 0) || strings.Contains(r.URL.RawPath, "%00") {
			writeProblem(w, r, notFoundResource, "no resource matches the requested path")
			return
		}
		if strings.ContainsRune(r.URL.RawQuery, 0) || strings.Contains(r.URL.RawQuery, "%00") {
			writeError(w, r, errNULQuery)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// errNULQuery is the malformed-request detail of a NUL in the query string.
var errNULQuery error = nulError{"a NUL character (U+0000) is not allowed in the query string"}

// nulError is a malformed-request error with its own detail.
type nulError struct{ detail string }

func (e nulError) Error() string { return e.detail }

func (e nulError) Is(target error) bool { return target == errMalformedRequest }
