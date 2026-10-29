package usecases

import (
	"context"
	"errors"

	"github.com/IQVO/product-discovery/internal/domain/search"
)

// Failure reasons of a search; stable, low-cardinality slugs (the problem type
// suffix the REST adapter answers with, or internal-error).
const (
	reasonValidationFailed = "validation-failed"
	reasonInternalError    = "internal-error"
)

// recordSearch counts one SearchProducts call: failed when err is set, empty
// for a first page without hits, served otherwise.
func (q Queries) recordSearch(ctx context.Context, page SearchPage, firstPage bool, err error) {
	switch {
	case err != nil:
		q.metrics().SearchFailed(ctx, searchFailureReason(err))
	case firstPage && len(page.Hits) == 0:
		q.metrics().SearchEmpty(ctx)
	default:
		q.metrics().SearchServed(ctx)
	}
}

func searchFailureReason(err error) string {
	if errors.Is(err, search.ErrValidation) {
		return reasonValidationFailed
	}
	return reasonInternalError
}
