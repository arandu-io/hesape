package model

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// ApplyListing narrows and orders the query as a listing state asks: the search
// text, every selected facet, and the sort followed by its tie-break. See
// query.Builder.ApplyListing for what it writes.
//
// Like Where and OrderBy it only builds. The tenant and the global scopes go on
// where the query runs.
func (b *Builder) ApplyListing(state pagination.ListingState) *Builder {
	b.query.ApplyListing(state)
	return b
}

// Listing runs the query as a listing state asks and returns the rows of the
// page beside the result a table draws from: the state, the page with its total
// and the count of every facet value. See query.Builder.Listing for the shape of
// the result.
//
// Every statement -- the page, the total and each facet count -- is prepared
// like any other read: the global scopes of the model go on, and the tenant of
// the Grant after them. A Grant without a tenant fails the call before the
// first. The builder it is called on is not changed.
//
// As on a page, the after-query callbacks do not run.
func (b *Builder) Listing(ctx context.Context, g auth.Grant, state pagination.ListingState, path string, columns ...any) (Rows, *pagination.ListingResult, error) {
	if state.Listing() == nil {
		return nil, nil, query.ErrNoListing
	}

	work := clone(b)
	work.query.ApplyListing(state)
	total, err := work.GetCountForPagination(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	state = state.Clamp(int(total))

	items := Rows{}
	if total > 0 {
		paged := clone(work)
		paged.query.ForPage(state.Page(), state.PerPage())
		items, err = paged.get(ctx, g, columns...)
		if err != nil {
			return nil, nil, err
		}
	}

	counts := map[string][]pagination.FacetCount{}
	for _, facet := range state.Listing().Facets() {
		counted, spec, _ := b.query.FacetCountQuery(state, facet.Key)
		counting := clone(b)
		counting.query = counted
		prepared, err := prepare(counting, g)
		if err != nil {
			return nil, nil, err
		}
		records, err := runSelect(prepared, ctx)
		if err != nil {
			return nil, nil, err
		}
		counts[facet.Key] = query.FacetCountsFrom(spec, state.Facet(facet.Key), records)
	}

	page := pagination.NewLengthAwarePage(len(items), int(total), state.PerPage(), state.Page(), state.PageOptions(path))
	return items, pagination.NewListingResult(state, page, counts), nil
}
