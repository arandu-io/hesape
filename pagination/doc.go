// Package pagination is Arandu's Illuminate\Pagination.
//
// It holds three paginators, one per question a list page can answer:
//
//   - [LengthAwarePaginator], from [Paginate]: knows the total, so it can say
//     "page 3 of 47" and print numbered links. It costs one COUNT query.
//   - [Paginator], from [SimplePaginate]: knows only whether one more row
//     exists, so it can say "previous" and "next" and nothing else. It costs
//     one extra row.
//   - [CursorPaginator], from [CursorPaginate]: keyset paging, forward and
//     backward, over an opaque [Cursor]. It has no total and no page number,
//     and it is the only one of the three that does not skip or repeat rows
//     when the data changes underneath.
//
// The files it answers to, in the clone at laravel_illuminate/pagination:
//
//	AbstractCursorPaginator.php
//	AbstractPaginator.php
//	Cursor.php
//	CursorPaginator.php
//	LengthAwarePaginator.php
//	PaginationServiceProvider.php
//	PaginationState.php
//	Paginator.php
//	UrlWindow.php
//
// # What a paginator is here
//
// A paginator is a value: the items of one page, plus the arithmetic needed to
// write the URL of the pages around it. It runs no query, holds no database
// handle and knows no Grant. A repository reads the rows -- through a Policy,
// like every other read (RULE 17) -- and hands them here.
//
// That division is why this package is a leaf. It imports the standard library
// and nothing else, and every dependency runs the other way: the repository
// knows about pagination, pagination knows nothing about repositories.
//
// # No global resolvers
//
// Illuminate reads the current page, the current path and the query string from
// four static closures installed by a service provider, which is what makes
// $users->links() work with no arguments in a Blade file. There is no container
// here (ADR 0001) and no facade (ADR 0002), so the request is read explicitly:
// [OptionsFrom] takes the *url.URL and returns the [Options] every constructor
// takes, and [CurrentPage] and [CurrentCursor] read the position out of it.
//
// # No rendering
//
// Illuminate ships nine Blade views and a useTailwind/useBootstrap switch.
// Rendering lives in the view layer here, and the closed set of it: a paginator
// exposes [LengthAwarePaginator.Links], a flat []Link the kyse Pagination
// component walks. Nothing in this package writes HTML.
//
// Links carries the numbered pages and the "..." separators only. Previous and
// next are [LengthAwarePaginator.PreviousPageURL] and
// [LengthAwarePaginator.NextPageURL], because their label is a translated word
// or an icon, and the core has no translator to ask.
//
// # No interfaces
//
// Illuminate\Contracts\Pagination declares Paginator and LengthAwarePaginator.
// They are absent on purpose: an interface belongs in the package that consumes
// it, and until something consumes one, declaring it here would only be a name
// to keep in step with the structs.
package pagination
