package query

import (
	"context"
	"errors"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/pagination"
)

// ErrNoListing is returned by the listing terminals for a state that no
// pagination.Listing read: the zero ListingState, which knows no columns and no
// page size.
var ErrNoListing = errors.New("query: the listing state was not read by a pagination.Listing")

// likeEscape is the character the search pattern escapes its wildcards with.
// Backslash is the usual choice and the wrong one here: it is the escape of
// MySQL string literals already, so `escape '\'` is not a statement every engine
// reads the same way. An exclamation mark is ordinary text on all of them.
const likeEscape = "!"

// facetValueAlias and facetCountAlias are the two columns a facet-count query
// selects. They are fixed names so that the rows read back the same on every
// engine, whatever the column is called or how the engine folds the case of an
// alias.
const (
	facetValueAlias = "facet_value"
	facetCountAlias = "facet_count"
)

// escapeLike makes text match itself inside a LIKE pattern, wildcards
// included.
func escapeLike(text string) string {
	return strings.NewReplacer(likeEscape, likeEscape+likeEscape, "%", likeEscape+"%", "_", likeEscape+"_").Replace(text)
}

// ApplyListing narrows and orders the query as a listing state asks: the search
// text, every selected facet, and the sort followed by its tie-break.
//
// Every column it names comes from the spec the state was read with, so nothing
// in a request reaches the statement as an identifier, and every value it
// compares against is a binding. The search is "contains", without regard to
// case, over the declared search columns joined with "or"; the wildcards of the
// text are escaped so that a "%" is looked for and not matched against. Each
// facet is an "in" over the values selected, joined to the rest with "and".
//
// It builds and runs nothing, like Where and OrderBy: the tenant goes on where
// the query is run. The search columns must hold text.
func (b *Builder) ApplyListing(state pagination.ListingState) *Builder {
	b.ApplyListingFilters(state, "")
	for _, order := range state.Order() {
		dir := "asc"
		if order.Desc {
			dir = "desc"
		}
		b.OrderBy(order.Column, dir)
	}
	return b
}

// ApplyListingFilters adds the search and the facets of a listing state, and
// leaves the facet under except out; an empty except leaves nothing out. It is
// the filtering half of ApplyListing, and what a facet count is made on.
func (b *Builder) ApplyListingFilters(state pagination.ListingState, except string) *Builder {
	if text := state.Search(); text != "" {
		columns := state.SearchColumns()
		pattern := "%" + escapeLike(text) + "%"
		if len(columns) > 0 {
			b.WhereNested(func(group *Builder) {
				for _, column := range columns {
					group.OrWhereRaw("lower("+b.Grammar.Wrap(column)+") like lower(?) escape '"+likeEscape+"'", pattern)
				}
			}, "and")
		}
	}
	for _, facet := range state.FacetFilters(except) {
		values := make([]any, len(facet.Values))
		for i, value := range facet.Values {
			values[i] = value
		}
		b.WhereIn(facet.Column, values)
	}
	return b
}

// FacetCountQuery returns the query that counts the rows of each value of the
// facet under key: this query's filters, plus the search and every facet of the
// state except the one being counted, grouped by the facet's column.
//
// Leaving the facet's own selection out is what lets a reader see how many rows
// the values they have not picked would add. The receiver is not changed. The
// query selects the columns "facet_value" and "facet_count", and has no order,
// limit or offset; run it with the Grant like any other, and read the rows with
// FacetCountsFrom. The second result is false for a key the spec did not
// declare.
func (b *Builder) FacetCountQuery(state pagination.ListingState, key string) (*Builder, pagination.FacetSpec, bool) {
	listing := state.Listing()
	if listing == nil {
		return nil, pagination.FacetSpec{}, false
	}
	facet, ok := listing.Facet(key)
	if !ok {
		return nil, pagination.FacetSpec{}, false
	}
	counted := b.CloneWithout("columns", "orders", "limit", "offset").CloneWithoutBindings("select", "order")
	counted.ApplyListingFilters(state, key)
	counted.Select(facet.Column + " as " + facetValueAlias)
	counted.SelectRaw("count(*) as " + facetCountAlias)
	counted.GroupBy(facet.Column)
	return counted, facet, true
}

// FacetCountsFrom reads the rows of a FacetCountQuery into the count of every
// value the facet lists, in the order it lists them. A value no row carries
// counts zero, and a value the rows carry that the facet does not list is left
// out.
func FacetCountsFrom(facet pagination.FacetSpec, selected []string, rows []Record) []pagination.FacetCount {
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		value, ok := row[facetValueAlias]
		if !ok || value == nil {
			continue
		}
		number, _ := castNumber(row[facetCountAlias])
		counts[facetText(value)] += int(number)
	}
	out := make([]pagination.FacetCount, len(facet.Values))
	for i, value := range facet.Values {
		out[i] = pagination.FacetCount{Value: value, Count: counts[value]}
		for _, chosen := range selected {
			if chosen == value {
				out[i].Selected = true
			}
		}
	}
	return out
}

// facetText is a facet value as text, whatever the driver returned: a text
// column comes back as a string from one driver and as bytes from another.
func facetText(value any) string {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return stringify(value)
}

// Listing runs the query as a listing state asks and returns the rows of the
// page beside the result a table draws from.
//
// The state narrows and orders the query (see ApplyListing); the rows are the
// state's page, and the total is the count of the rows that match, before the
// page. A state on a page past the last one is brought back to the last page,
// and the result carries the state that was read, so a link written from it is
// the page the reader sees. For each facet the result carries the count of
// every value, with every filter but the facet's own applied.
//
// path is the address the listing is served at; the links of the result's page
// are built on it and keep every parameter of the state.
//
// Every statement it issues is run by Get, so each carries the tenant of g: the
// page, the total, and every facet count. A Grant without a tenant fails the
// call before the first one. The query itself is not changed.
func (b *Builder) Listing(ctx context.Context, g auth.Grant, state pagination.ListingState, path string, columns ...any) ([]Record, *pagination.ListingResult, error) {
	if state.Listing() == nil {
		return nil, nil, ErrNoListing
	}

	work := b.Clone().ApplyListing(state)
	total, err := work.GetCountForPagination(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	state = state.Clamp(total)

	var rows []Record
	if total > 0 {
		rows, err = work.ForPage(state.Page(), state.PerPage()).Get(ctx, g, columns...)
		if err != nil {
			return nil, nil, err
		}
	}

	counts := map[string][]pagination.FacetCount{}
	for _, facet := range state.Listing().Facets() {
		counted, spec, _ := b.FacetCountQuery(state, facet.Key)
		found, err := counted.Get(ctx, g)
		if err != nil {
			return nil, nil, err
		}
		counts[facet.Key] = FacetCountsFrom(spec, state.Facet(facet.Key), found)
	}

	page := pagination.NewLengthAwarePage(len(rows), total, state.PerPage(), state.Page(), state.PageOptions(path))
	return rows, pagination.NewListingResult(state, page, counts), nil
}
