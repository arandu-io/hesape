// Package pagination turns a page of rows into the values a pager renders from.
//
// It holds three pages, one per question a list page can answer, and the state
// of a listing that asks them:
//
//   - [LengthAwarePage], from [NewLengthAwarePage]: knows the total, so it can
//     say "page 3 of 47" and print numbered links. It costs one COUNT query.
//   - [Page], from [NewPage]: knows only whether one more row exists, so it can
//     say "previous" and "next" and nothing else. It costs one extra row.
//   - [CursorPage], from [NewCursorPage]: keyset paging, forward and backward,
//     over a signed [Cursor]. It has no total and no page number, and it is the
//     only one of the three that does not skip or repeat rows when the data
//     changes underneath.
//
// # What a page is here
//
// A page is a value: the arithmetic needed to say where one page of a result
// set sits and to write the URL of the pages around it. It runs no query, holds
// no database handle and knows no Grant. A repository reads the rows -- through
// a Policy, like every other read -- and tells the page how many it read.
//
// It does not hold the rows either. The rows travel beside the page, as
// whatever type the query returned them as, and the page is the same type for
// every kind of row. A page that carried its rows would be a type with a type
// parameter, compiled again for every kind of row in every package that names
// it; this one is compiled once. The payload methods take the rows as an
// argument for the same reason -- ToArray(rows) puts them under "data".
//
// That division is why this package is a leaf. It imports the standard library
// and the application key, and every dependency runs the other way: the
// repository knows about pagination, pagination knows nothing about
// repositories.
//
// # A listing is declared
//
// A table on screen is more than a page: it has a search box, a sort, filters
// with a count per value, a page size and columns the reader hides, and all of
// it lives in the query string. [ListingSpec] declares what a listing allows --
// the searchable columns, the sorts as public keys mapped to columns, the
// facets and the values each lists, the page sizes, the hideable columns, the
// names of the parameters -- and [NewListing] checks it. [Listing.Parse] reads a
// request's query into a [ListingState], and the state holds only what the spec
// declared: a sort, a facet value, a page size or a column the spec does not
// know falls back to the default, and nothing in the request is ever an
// identifier.
//
// The state is a value. Its methods return the state a click leads to --
// [ListingState.ToggleSort], [ListingState.ToggleFacet],
// [ListingState.WithPageSize], [ListingState.ToggleHidden] -- applying the rules
// of the table, and [ListingState.Values] writes it back as one canonical query
// string, so a link built from a state never gathers a parameter. The query
// builder applies the state to a query and runs it under a Grant, which is where
// the rows, the total and the facet counts come from; this package only holds
// the vocabulary, and runs nothing.
//
// # The cursor is signed
//
// A page number is a number, and a reader who edits one reaches a page they
// could have reached by typing it. A cursor is the boundary row of a page, sent
// to the client and handed back, so a reader who edits one names the row the
// next query starts at. [CursorSigner] is what closes that: it signs the token
// on the way out and checks it on the way in, and it is the only way a [Cursor]
// becomes a string.
//
// # No global resolvers
//
// The request is read explicitly. [OptionsFrom] takes the *url.URL and returns
// the [Options] every constructor takes, and [ResolveCurrentPage],
// [ResolveCurrentPath], [ResolveQueryString] and [ResolveCurrentCursor] read
// the four pieces out of it -- the last of them against the [CursorSigner], the
// cursor being checked before it is read. There is nothing to install and
// nothing to install it from.
//
// # The views are names here, and components in the view layer
//
// [DefaultView], [DefaultSimpleView], [UseTailwind], [UseBootstrapFive] and
// their siblings name a pager, and a name is all they are. Nothing in this
// package writes HTML: rendering belongs to the view layer.
//
// What a component renders from is [LengthAwarePage.Links]: a flat []Link
// holding the numbered pages and a [Separator] wherever the window left pages
// out, in the order a pager draws them. [LengthAwarePage.LinkCollection] is the
// same list with previous and next at its ends, and is what the JSON payload
// carries.
//
// # No interfaces
//
// There is no Paginator interface, on purpose: an interface belongs in the
// package that consumes it, and until something consumes one, declaring it here
// would only be a name to keep in step with the structs.
//
// # Naming
//
// An initialism is spelled in one case, so [LengthAwarePage.URL] and
// [LengthAwarePage.ToJSON].
package pagination
