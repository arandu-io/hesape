package pagination

import (
	"encoding/json"
	"iter"
	"net/url"
	"strconv"
)

// LengthAwarePaginator is Illuminate's Pagination\LengthAwarePaginator: a page
// of a result set whose size is known.
//
// It is what paginate() returns, and knowing the total is the whole difference:
// it can print "page 3 of 47", it can link the last page, and it can render a
// numbered window. That knowledge is bought with a COUNT over the same
// predicate as the page query, which on a large table is the more expensive of
// the two.
//
// Build one with Paginate.
type LengthAwarePaginator[T any] struct {
	items       []T
	total       int
	perPage     int
	currentPage int
	lastPage    int
	onEachSide  int
	options     Options
}

// Paginate is LengthAwarePaginator::__construct. It returns the page
// holding items, out of total rows.
//
// The items are the rows of this page as the repository read them; nothing is
// sliced here. total is the row count of the whole result set, and currentPage
// is the page those rows were read for -- normally ResolveCurrentPage of the
// request URL.
//
// perPage below one is read as one, and currentPage below one as one. Neither
// is a caller's mistake worth an error: they are what an empty configuration
// value and a hand-edited URL produce, and a paginator that returns an error
// instead of a page turns both into a 500.
//
// A currentPage past the last one is kept rather than clamped, because that is
// what Illuminate does and what a reader who deleted the last row of the last
// page sees: an empty page, with a working link back.
func Paginate[T any](items []T, total, perPage, currentPage int, opts Options) *LengthAwarePaginator[T] {
	if perPage < 1 {
		perPage = 1
	}
	if currentPage < 1 {
		currentPage = 1
	}
	if total < 0 {
		total = 0
	}

	lastPage := (total + perPage - 1) / perPage
	if lastPage < 1 {
		lastPage = 1
	}

	normalized := opts.normalize()
	return &LengthAwarePaginator[T]{
		items:       items,
		total:       total,
		perPage:     perPage,
		currentPage: currentPage,
		lastPage:    lastPage,
		onEachSide:  normalized.OnEachSide,
		options:     normalized,
	}
}

// Items is AbstractPaginator::items. It returns the rows of this page, in the order they
// were read.
func (p *LengthAwarePaginator[T]) Items() []T { return p.items }

// Count is AbstractPaginator::count, which is PHP's Countable. It returns how many rows this page holds, which is at
// most PerPage and is less on the last page.
func (p *LengthAwarePaginator[T]) Count() int { return len(p.items) }

// IsEmpty is AbstractPaginator::isEmpty. It reports whether this page holds no rows.
func (p *LengthAwarePaginator[T]) IsEmpty() bool { return len(p.items) == 0 }

// IsNotEmpty is AbstractPaginator::isNotEmpty. It reports whether this page holds any rows.
func (p *LengthAwarePaginator[T]) IsNotEmpty() bool { return len(p.items) > 0 }

// GetIterator is AbstractPaginator::getIterator. It ranges over the rows of this page with
// their offsets.
//
// PHP returns an ArrayIterator because IteratorAggregate demands one; Go's
// range-over-func is the same thing under another name.
func (p *LengthAwarePaginator[T]) GetIterator() iter.Seq2[int, T] { return seq2(p.items) }

// GetCollection is AbstractPaginator::getCollection. It returns the rows this page holds.
//
// Illuminate distinguishes the Collection from the plain array items() returns;
// here both are the same slice, and both names are kept because both are called.
func (p *LengthAwarePaginator[T]) GetCollection() []T { return p.items }

// SetCollection is AbstractPaginator::setCollection. It replaces the rows this page holds,
// leaving every number -- total, last page, current page -- alone.
func (p *LengthAwarePaginator[T]) SetCollection(items []T) *LengthAwarePaginator[T] {
	p.items = items
	return p
}

// GetOptions is AbstractPaginator::getOptions. It returns the options this page builds its
// URLs from, with the defaults already applied.
func (p *LengthAwarePaginator[T]) GetOptions() Options { return p.options }

// Total is LengthAwarePaginator::total. It returns how many rows the whole result set holds.
func (p *LengthAwarePaginator[T]) Total() int { return p.total }

// PerPage is AbstractPaginator::perPage. It returns the page size the paginator was built
// with.
func (p *LengthAwarePaginator[T]) PerPage() int { return p.perPage }

// CurrentPage is AbstractPaginator::currentPage. It returns the page being read, counting
// from one.
func (p *LengthAwarePaginator[T]) CurrentPage() int { return p.currentPage }

// LastPage is LengthAwarePaginator::lastPage. It returns the number of the final page, and is
// at least one -- an empty result set still has a page one to show the reader.
func (p *LengthAwarePaginator[T]) LastPage() int { return p.lastPage }

// FirstItem is AbstractPaginator::firstItem. It returns the one-based index, in the whole
// result set, of the first row on this page.
//
// It is zero when the page is empty, where PHP returns null: Go has no null int
// and a nullable one would make every caller unwrap a number it prints.
func (p *LengthAwarePaginator[T]) FirstItem() int {
	if len(p.items) == 0 {
		return 0
	}
	return (p.currentPage-1)*p.perPage + 1
}

// LastItem is AbstractPaginator::lastItem. It returns the one-based index, in the whole
// result set, of the last row on this page, and zero when the page is empty.
//
// FirstItem and LastItem are the two numbers in "showing 21 to 40 of 512".
func (p *LengthAwarePaginator[T]) LastItem() int {
	if len(p.items) == 0 {
		return 0
	}
	return p.FirstItem() + len(p.items) - 1
}

// HasPages is AbstractPaginator::hasPages. It reports whether there is anywhere to go from
// here, which is the test for rendering a pager at all.
//
// It is not "more than one page": a reader who followed a stale link to page 4
// of a result set that now has one page is not on page one, and has somewhere
// to go back to.
func (p *LengthAwarePaginator[T]) HasPages() bool {
	return p.currentPage != 1 || p.HasMorePages()
}

// HasMorePages is LengthAwarePaginator::hasMorePages. It reports whether a page follows this
// one.
func (p *LengthAwarePaginator[T]) HasMorePages() bool { return p.currentPage < p.lastPage }

// OnFirstPage is AbstractPaginator::onFirstPage. It reports whether this is page one.
func (p *LengthAwarePaginator[T]) OnFirstPage() bool { return p.currentPage <= 1 }

// OnLastPage is AbstractPaginator::onLastPage. It reports whether this is the final page.
func (p *LengthAwarePaginator[T]) OnLastPage() bool { return !p.HasMorePages() }

// URL is AbstractPaginator::url. It returns the address of the given page; a page below one
// is read as one.
func (p *LengthAwarePaginator[T]) URL(page int) string {
	if page < 1 {
		page = 1
	}
	return p.options.url(p.options.PageName, strconv.Itoa(page))
}

// GetURLRange is AbstractPaginator::getUrlRange. It returns the address of every page from
// start to end inclusive, keyed by page number.
func (p *LengthAwarePaginator[T]) GetURLRange(start, end int) map[int]string {
	return p.urlsFor(pageRange(start, end))
}

// urlsFor is GetURLRange over a list of pages rather than a range, and returns
// nil for nothing -- which is the null UrlWindow puts in a piece that does not
// apply.
func (p *LengthAwarePaginator[T]) urlsFor(pages []int) map[int]string {
	if len(pages) == 0 {
		return nil
	}
	out := make(map[int]string, len(pages))
	for _, page := range pages {
		out[page] = p.URL(page)
	}
	return out
}

// PreviousPageURL is AbstractPaginator::previousPageUrl. It returns the address of the page
// before this one, and the empty string on page one, where PHP returns null.
func (p *LengthAwarePaginator[T]) PreviousPageURL() string {
	if p.currentPage <= 1 {
		return ""
	}
	return p.URL(p.currentPage - 1)
}

// NextPageURL is LengthAwarePaginator::nextPageUrl. It returns the address of the page after
// this one, and the empty string on the last page.
func (p *LengthAwarePaginator[T]) NextPageURL() string {
	if !p.HasMorePages() {
		return ""
	}
	return p.URL(p.currentPage + 1)
}

// Path is AbstractPaginator::path. It returns the base path the page links are built on.
func (p *LengthAwarePaginator[T]) Path() string { return p.options.Path }

// SetPath is AbstractPaginator::setPath. It sets the base path the page links are built on.
func (p *LengthAwarePaginator[T]) SetPath(path string) *LengthAwarePaginator[T] {
	p.options.Path = path
	return p
}

// WithPath is AbstractPaginator::withPath, which in Illuminate is setPath under the name
// the fluent calls read better with.
func (p *LengthAwarePaginator[T]) WithPath(path string) *LengthAwarePaginator[T] {
	return p.SetPath(path)
}

// GetPageName is AbstractPaginator::getPageName. It returns the query parameter the page
// number is written into.
func (p *LengthAwarePaginator[T]) GetPageName() string { return p.options.PageName }

// SetPageName is AbstractPaginator::setPageName. It sets the query parameter the page
// number is written into, which is how two paginators appear on one screen
// without moving each other.
func (p *LengthAwarePaginator[T]) SetPageName(name string) *LengthAwarePaginator[T] {
	p.options.PageName = name
	return p
}

// OnEachSide is AbstractPaginator::onEachSide. It sets how many numbered links sit either
// side of the current page before the window collapses into a separator.
//
// Illuminate exposes this as a public property as well as a setter; only the
// setter is kept, so that there is one way to change it.
func (p *LengthAwarePaginator[T]) OnEachSide(count int) *LengthAwarePaginator[T] {
	if count < 0 {
		count = 0
	}
	p.onEachSide = count
	p.options.OnEachSide = count
	return p
}

// Fragment is AbstractPaginator::fragment. It sets the fragment appended after a "#", so a
// page link can land on the table rather than the top of the document.
//
// PHP's fragment() is a getter when called with no argument and a setter
// otherwise, on one name. Go has neither optional parameters nor a union
// return, so this is the setter -- the form the fluent calls use -- and the
// fragment is read back through GetOptions. An empty string clears it.
func (p *LengthAwarePaginator[T]) Fragment(fragment string) *LengthAwarePaginator[T] {
	p.options.Fragment = fragment
	return p
}

// Appends is AbstractPaginator::appends. It carries extra query string values onto every
// generated URL, which is what keeps a filter or a sort selected while the
// reader walks the pages.
//
// PHP declares the first parameter as array|string|null. Go has no union type,
// so key is a string naming one parameter -- with its value as the second
// argument -- or a map[string]string, a url.Values or a map[string][]string
// naming several. Anything else, nil included, is ignored, exactly as PHP
// ignores null.
//
// The page parameter is never appended: the paginator writes that itself.
func (p *LengthAwarePaginator[T]) Appends(key any, value ...string) *LengthAwarePaginator[T] {
	appendQuery(&p.options, p.options.PageName, key, value)
	return p
}

// WithQueryString is AbstractPaginator::withQueryString. It carries every parameter of the
// current request onto the generated URLs.
//
// Illuminate reads them from a static closure a service provider installs;
// there is no container here (ADR 0001), so they are passed in -- normally
// Query of the request URL. The page parameter is dropped.
func (p *LengthAwarePaginator[T]) WithQueryString(query url.Values) *LengthAwarePaginator[T] {
	mergeQuery(&p.options, p.options.PageName, query)
	return p
}

// Links is the data behind LengthAwarePaginator::links: the numbered window a pager renders, being
// the pages near this one, the first two and the last two, and a Separator
// wherever pages were left out.
//
// Illuminate's links() returns rendered HTML from a Blade view it fetches
// through the view factory resolver. Nothing is rendered here: the view layer
// owns that, and a kyse Pagination component walks this slice.
//
// Previous and next are not in here, matching the elements Illuminate passes to
// the view. Their label is a translated word or an icon, and this package has
// no translator to ask; PreviousPageURL and NextPageURL are what the component
// draws them from. LinkCollection is the list that does include them.
func (p *LengthAwarePaginator[T]) Links() []Link {
	first, slider, last := NewURLWindow(p).pages()

	links := make([]Link, 0, len(first)+len(slider)+len(last)+2)
	appendPages := func(pages []int) {
		for _, page := range pages {
			links = append(links, Link{
				URL:    p.URL(page),
				Label:  strconv.Itoa(page),
				Page:   page,
				Active: page == p.currentPage,
			})
		}
	}

	appendPages(first)
	if len(slider) > 0 {
		links = append(links, Link{Label: Separator})
		appendPages(slider)
	}
	if len(last) > 0 {
		links = append(links, Link{Label: Separator})
		appendPages(last)
	}
	return links
}

// LinkCollection is LengthAwarePaginator::linkCollection. It is Links with the previous step
// prepended and the next step appended, which is the "links" array of the JSON
// payload.
//
// The two steps are labelled Previous and Next, which is the fallback
// Illuminate uses when there is no translator bound. A step with nowhere to go
// has an empty URL and a Page of zero, which MarshalJSON writes as null.
func (p *LengthAwarePaginator[T]) LinkCollection() []Link {
	numbered := p.Links()
	links := make([]Link, 0, len(numbered)+2)

	previous := Link{URL: p.PreviousPageURL(), Label: PreviousLabel}
	if p.currentPage > 1 {
		previous.Page = p.currentPage - 1
	}
	links = append(links, previous)
	links = append(links, numbered...)

	next := Link{URL: p.NextPageURL(), Label: NextLabel}
	if p.HasMorePages() {
		next.Page = p.currentPage + 1
	}
	return append(links, next)
}

// ToArray is LengthAwarePaginator::toArray. It is the payload Illuminate serialises a
// length-aware page to, key for key.
func (p *LengthAwarePaginator[T]) ToArray() map[string]any {
	return map[string]any{
		"current_page":   p.currentPage,
		"data":           p.items,
		"first_page_url": p.URL(1),
		"from":           nullable(p.FirstItem()),
		"last_page":      p.lastPage,
		"last_page_url":  p.URL(p.lastPage),
		"links":          p.LinkCollection(),
		"next_page_url":  nullable(p.NextPageURL()),
		"path":           p.Path(),
		"per_page":       p.perPage,
		"prev_page_url":  nullable(p.PreviousPageURL()),
		"to":             nullable(p.LastItem()),
		"total":          p.total,
	}
}

// MarshalJSON is LengthAwarePaginator::jsonSerialize, which is PHP's JsonSerializable; Go's
// name for the same contract is json.Marshaler. It makes the paginator itself
// encodable, so a handler can hand one to a JSON encoder.
func (p *LengthAwarePaginator[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ToArray())
}

// ToJSON is LengthAwarePaginator::toJson. PHP takes json_encode flags and returns a string;
// this returns the bytes and the error json.Marshal reports, which is where the
// exception went.
func (p *LengthAwarePaginator[T]) ToJSON() ([]byte, error) {
	return json.Marshal(p.ToArray())
}

// ToPrettyJSON is LengthAwarePaginator::toPrettyJson. It is ToJSON indented four spaces, as
// PHP's JSON_PRETTY_PRINT indents.
func (p *LengthAwarePaginator[T]) ToPrettyJSON() ([]byte, error) {
	return prettyJSON(p.ToArray())
}

// Through is AbstractPaginator::through: the same page with every item passed through f,
// which is how a page of database rows becomes a page of whatever the view is
// written against without recomputing a single number.
//
// It is a function rather than a method because a Go method cannot introduce a
// type parameter, and the point of this one is to change the element type --
// which is what PHP's through does too, mutating the collection in place.
// ThroughSimple and ThroughCursor are the same operation on the other two
// paginators; three names, because Go cannot overload one.
func Through[A, B any](p *LengthAwarePaginator[A], f func(A) B) *LengthAwarePaginator[B] {
	items := make([]B, len(p.items))
	for i, item := range p.items {
		items[i] = f(item)
	}
	return &LengthAwarePaginator[B]{
		items:       items,
		total:       p.total,
		perPage:     p.perPage,
		currentPage: p.currentPage,
		lastPage:    p.lastPage,
		onEachSide:  p.onEachSide,
		options:     p.options,
	}
}
