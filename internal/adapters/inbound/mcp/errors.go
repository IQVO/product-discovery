package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// errorCatalogue names each typed error a read tool can meet with the same
// stable slug the REST adapter uses as the last segment of its RFC 9457 "type"
// URI (internal/adapters/inbound/http/errors.go), so a client sees one error
// vocabulary over both surfaces. Only the errors a READ can raise are listed,
// in match order (a cursor error is also a validation error, so it comes
// first): a cursor that is not a page token of this search, a miss of the
// repository and a bad search value. A detail is adapter-owned text when it
// must not echo the domain's wording (the cursor field is named `after` there,
// a name this surface does not have).
var errorCatalogue = []struct {
	target error
	slug   string
	detail string
}{
	{search.ErrInvalidCursor, "invalid-cursor", "cursor is not a valid page token"},
	{ports.ErrNotFound, "not-found", ""},
	{search.ErrValidation, "validation-failed", ""},
}

// toolError builds a tool-level error "<slug>: <detail>". Returned from a
// handler it becomes an isError tool result, never a transport failure.
func toolError(slug, detail string) error {
	return fmt.Errorf("%s: %s", slug, detail)
}

// mapError turns a use-case/repository error into the tool error the model
// sees. Typed errors are prefixed with their REST slug; anything else is logged
// and reported generically so infrastructure details (DSNs, SQL) never reach
// the model.
func mapError(ctx context.Context, err error) error {
	for _, entry := range errorCatalogue {
		if errors.Is(err, entry.target) {
			if entry.detail != "" {
				return toolError(entry.slug, entry.detail)
			}
			return toolError(entry.slug, err.Error())
		}
	}
	slog.ErrorContext(ctx, "mcp tool failed with an unexpected error", "error", err)
	return toolError("internal-error", "an unexpected internal error occurred")
}

// isNotFound reports whether err is the repository's miss (or a stub, which the
// use case also answers as ports.ErrNotFound).
func isNotFound(err error) bool { return errors.Is(err, ports.ErrNotFound) }

// notFound is the not-found answer for an identifier: adapter-owned text that
// bounds the echoed identifier so an over-long one is not reflected in full.
func notFound(id string) error {
	return toolError("not-found", fmt.Sprintf("no product exists with catalogItemId %q", truncate(id)))
}

// truncate bounds an echoed identifier.
func truncate(s string) string {
	const limit = 64
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "..."
	}
	return s
}
