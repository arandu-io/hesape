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
// The files it answers to, in the clone at laravel_illuminate/pagination,
// which is the source this package was written from (Laravel 13,
// illuminate/pagination ^13.0):
//
//	AbstractCursorPaginator.php     -> CursorPaginator
//	AbstractPaginator.php           -> LengthAwarePaginator, Paginator
//	Cursor.php                      -> Cursor
//	CursorPaginator.php             -> CursorPaginator
//	LengthAwarePaginator.php        -> LengthAwarePaginator
//	PaginationServiceProvider.php   -> nothing (ADR 0001, ADR 0002)
//	PaginationState.php             -> OptionsFrom
//	Paginator.php                   -> Paginator
//	UrlWindow.php                   -> URLWindow, URLWindowRanges, Make
//	resources/views/*.blade.php     -> DefaultView and the five Use* switches,
//	                                   as names; the drawing is the view layer
//
// # The names are Illuminate's
//
// Every method carries the name its PHP counterpart carries, and the
// alterations are these: a name is capitalised to export it; an initialism is
// spelled in one case, so url() is [LengthAwarePaginator.URL] and toJson() is
// [LengthAwarePaginator.ToJSON]; where PHP throws, the Go method returns an
// error; and where a static method carries its class in the identifier, the
// class is gone, because Go has no static methods -- Cursor::fromEncoded is
// [FromEncoded] and UrlWindow::make is [Make].
//
// Three names are functions rather than methods, because a Go method cannot
// introduce a type parameter and the point of these three is to change the
// element type: [Through], [ThroughSimple] and [ThroughCursor] are PHP's
// through() on the three paginators. One name each, because Go cannot overload.
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
// Illuminate reads the current page, the current path, the query string and the
// current cursor from static closures a service provider installs through
// PaginationState::resolveUsing, which is what makes $users->links() work with
// no arguments in a Blade file. There is no container here (ADR 0001) and no
// facade (ADR 0002), so the request is read explicitly: [OptionsFrom] takes the
// *url.URL and returns the [Options] every constructor takes, and
// [ResolveCurrentPage], [ResolveCurrentPath], [ResolveQueryString] and
// [ResolveCurrentCursor] read the four pieces out of it.
//
// The five closure setters -- AbstractPaginator::currentPageResolver,
// AbstractPaginator::currentPathResolver, AbstractPaginator::queryStringResolver,
// AbstractPaginator::viewFactoryResolver and
// AbstractCursorPaginator::currentCursorResolver -- and
// PaginationState::resolveUsing itself are the container wiring, and are not
// here. They are listed with their reason under "The methods with no answer
// here" below.
//
// PaginationServiceProvider::register and PaginationServiceProvider::boot go
// with them, both reason 2 of the porting rule: register is the single call to
// PaginationState::resolveUsing($this->app) that installs those closures, and
// boot registers the 'pagination' Blade view namespace and publishes the nine
// files into the application. There is no view namespace to register here --
// the view layer resolves a component by name at build time -- and nothing to
// publish, because the pager is a kyse component the application already has.
//
// # The views are names here and components in the view layer
//
// Illuminate ships nine Blade files and a switch between them.
// [DefaultView], [DefaultSimpleView], [UseTailwind] and [UseBootstrapFive] and
// its siblings are here, and they carry the same nine names -- but a name is
// all they are. Nothing in this package writes HTML: AbstractPaginator::render,
// AbstractPaginator::toHtml and AbstractPaginator::viewFactory all end at a view
// factory pulled out of the container, which ADR 0001 removed, and rendering
// belongs to the view layer either way.
//
// What a component renders from is [LengthAwarePaginator.Links]: a flat []Link
// holding the numbered pages and a [Separator] wherever the window left pages
// out, which is the elements array Illuminate passes to the Blade file, in the
// order a pager draws them. [LengthAwarePaginator.LinkCollection] is the same
// list with previous and next at its ends, and is what the JSON payload
// carries.
//
// bootstrap-5.blade.php is the one that maps onto the kyse pagination component
// with nothing left over: it reads hasPages, onFirstPage, previousPageUrl,
// hasMorePages, nextPageUrl, firstItem, lastItem and total off the paginator,
// and walks the elements printing a page number or the separator. The component
// takes the paginator and calls the same methods -- there is no @lang, because
// the labels come from the translation package, and no is_string test for the
// separator, because a [Link] with an empty URL is one.
//
// # No interfaces
//
// Illuminate\Contracts\Pagination declares Paginator and LengthAwarePaginator.
// They are absent on purpose: an interface belongs in the package that consumes
// it, and until something consumes one, declaring it here would only be a name
// to keep in step with the structs.
//
// # The methods with no answer here
//
// Every one of them, with the numbered reason from ADR 0056: (1) a PHP language
// feature Go does not have, (2) a method that only serves the container, a
// facade or a service provider, (3) a driver this ecosystem does not carry.
//
// Reason 1, the PHP language:
//
//   - AbstractPaginator::offsetExists, ::offsetGet, ::offsetSet, ::offsetUnset
//     and the same four on AbstractCursorPaginator are ArrayAccess, which is how
//     PHP writes $page[3]. Go indexes the slice [LengthAwarePaginator.Items]
//     returns.
//   - AbstractPaginator::__call and AbstractCursorPaginator::__call forward an
//     unknown method to the underlying Collection. That is the magic-method
//     family.
//   - AbstractPaginator::__toString, AbstractCursorPaginator::__toString and
//     AbstractPaginator::escapeWhenCastingToString, the flag that configures the
//     first two. Go has no cast to string to hook.
//   - AbstractPaginator::jsonSerialize and its three siblings are
//     JsonSerializable; Go's name for that contract is json.Marshaler, and
//     MarshalJSON is it, so the method is here under Go's spelling for the same
//     interface.
//   - AbstractPaginator::getIterator is IteratorAggregate; the Go spelling is
//     the range-over-func [LengthAwarePaginator.GetIterator] returns, and it is
//     here too.
//
// Reason 2, the container and the service provider:
//
//   - AbstractPaginator::currentPageResolver, AbstractPaginator::currentPathResolver
//     and AbstractCursorPaginator::currentCursorResolver install static closures
//     that read the request out of the application. [OptionsFrom] takes the
//     *url.URL instead.
//   - AbstractPaginator::queryStringResolver installs the closure that answers
//     `$app['request']->query()`. Its only caller is resolveQueryString, and
//     [ResolveQueryString] reads the query off the *url.URL it is handed, so
//     there is no static to set and nothing to set it from.
//   - AbstractPaginator::viewFactoryResolver installs the closure that answers
//     `$app['view']`. Nothing here renders -- see the section above -- so there
//     is no factory to resolve; a component takes the paginator and reads
//     [LengthAwarePaginator.Links] off it.
//   - PaginationState::resolveUsing is the single call that installs those five,
//     and it is the only caller any of them has.
//   - AbstractPaginator::viewFactory and AbstractCursorPaginator::viewFactory
//     pull the view factory out of the container.
//   - LengthAwarePaginator::links, ::render, CursorPaginator::links, ::render,
//     Paginator::links, ::render, AbstractPaginator::toHtml and
//     AbstractCursorPaginator::toHtml all end at that factory.
//     [LengthAwarePaginator.Links] and [LengthAwarePaginator.LinkCollection] are
//     the data they render from; the drawing belongs to the view layer.
//   - PaginationServiceProvider::register is that one call to
//     PaginationState::resolveUsing, and ::boot registers the 'pagination' Blade
//     view namespace and publishes the nine files. There is no view namespace
//     here -- the view layer resolves a component by name at build time -- and
//     nothing to publish, because the pager is a kyse component the application
//     already has.
//
// Reason 3, a driver this ecosystem does not carry:
//
//   - AbstractPaginator::loadMorph, ::loadMorphCount and the same two on
//     AbstractCursorPaginator forward to Eloquent polymorphic relations. There
//     is no Eloquent here, and 00-meta/DOC-architecture.md rejects Active Record.
//
// The protected members answer to nothing on purpose: AbstractPaginator::
// isValidPageNumber, ::appendArray, ::addQuery, ::buildFragment,
// ::setCurrentPage, ::setItems, ::elements; AbstractCursorPaginator::
// getPivotParameterForItem, ::ensureParameterIsPrimitive, ::appendArray,
// ::addQuery, ::buildFragment, ::setItems; UrlWindow::getSmallSlider,
// ::getUrlSlider, ::getSliderTooCloseToBeginning, ::getSliderTooCloseToEnding,
// ::getFullSlider, ::currentPage, ::lastPage. Each is the body of an exported
// method above it, and each is unexported here for the reason it is protected
// there.
package pagination
