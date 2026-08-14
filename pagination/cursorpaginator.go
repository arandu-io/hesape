package pagination

import (
	"encoding/json"
	"errors"
	"iter"
	"net/url"
)

// CursorPaginator is a page of a keyset walk: the rows on one side of a Cursor,
// and the cursors that reach the pages either side of it.
//
// It is what Illuminate's cursorPaginate() returns, and it is the one paginator
// whose pages hold still. There is no total and no page number -- naming a page
// by number means counting rows, and counting rows is the cost keyset paging
// exists to avoid. What it offers is previous and next, both of them exact.
//
// Build one with CursorPaginate.
type CursorPaginator[T any] struct {
	items    []T
	perPage  int
	cursor   *Cursor
	hasMore  bool
	previous *Cursor
	next     *Cursor
	key      func(T) map[string]string
	options  Options
}

// CursorPaginate is CursorPaginator::__construct. It returns the page holding
// items.
//
// The repository reads perPage+1 rows from the boundary named by cursor -- nil
// for the first page -- and hands all of them here. The extra row answers "is
// there another page" and is dropped before anybody sees it.
//
// A cursor whose PointsToNext is false was read backwards, so its rows arrive
// in reverse order; they are turned around here, and the caller does not have
// to remember. That is also why the probe row is dropped before the reversal:
// on a backward page the extra row is the one furthest from the boundary, which
// is the first one the reader would have seen.
//
// key is how a row becomes a cursor: given an item it returns the value of each
// column the query orders by, keyed by column name. It must name the same
// columns as the ORDER BY, and it must format them so that parsing them back
// yields the same value -- a truncated timestamp walks past every row that
// shares it. It is required, and CursorPaginate panics on a nil key: a
// paginator that quietly produced no cursors would render a pager with no way
// forward and no error to explain it.
func CursorPaginate[T any](items []T, perPage int, cursor *Cursor, key func(T) map[string]string, opts Options) *CursorPaginator[T] {
	if key == nil {
		panic("pagination: CursorPaginate needs a key function to build cursors from rows")
	}
	if perPage < 1 {
		perPage = 1
	}

	hasMore := len(items) > perPage
	if hasMore {
		items = items[:perPage]
	}
	backwards := cursor != nil && !cursor.PointsToNextItems()
	if backwards {
		reversed := make([]T, len(items))
		for i, item := range items {
			reversed[len(items)-1-i] = item
		}
		items = reversed
	}

	p := &CursorPaginator[T]{
		items:   items,
		perPage: perPage,
		cursor:  cursor,
		hasMore: hasMore,
		key:     key,
		options: opts.normalize(),
	}
	if len(items) > 0 {
		// A backward page that found no extra row has reached the start of the
		// result set, so there is nothing before it; every other page has a
		// row to point back at. Symmetrically forward, with hasMore.
		if !(cursor == nil || (backwards && !hasMore)) {
			previous := NewCursor(key(items[0]), false)
			p.previous = &previous
		}
		if hasMore || backwards {
			next := NewCursor(key(items[len(items)-1]), true)
			p.next = &next
		}
	}
	return p
}

// Items is AbstractCursorPaginator::items. It returns the rows of this page, in
// reading order, with the probe row already removed.
func (p *CursorPaginator[T]) Items() []T { return p.items }

// Count is AbstractCursorPaginator::count, which is PHP's Countable. It returns
// how many rows this page holds.
func (p *CursorPaginator[T]) Count() int { return len(p.items) }

// PerPage is AbstractCursorPaginator::perPage. It returns the page size the
// paginator was built with.
func (p *CursorPaginator[T]) PerPage() int { return p.perPage }

// Cursor is AbstractCursorPaginator::cursor. It returns the cursor this page was
// read from, and nil on the first page.
func (p *CursorPaginator[T]) Cursor() *Cursor { return p.cursor }

// PreviousCursor is AbstractCursorPaginator::previousCursor. It returns the
// cursor that reads the page before this one, and nil when there is none.
func (p *CursorPaginator[T]) PreviousCursor() *Cursor { return p.previous }

// NextCursor is AbstractCursorPaginator::nextCursor. It returns the cursor that
// reads the page after this one, and nil when there is none.
func (p *CursorPaginator[T]) NextCursor() *Cursor { return p.next }

// HasMorePages is CursorPaginator::hasMorePages. It reports whether a page
// follows this one.
func (p *CursorPaginator[T]) HasMorePages() bool { return p.next != nil }

// HasPages is CursorPaginator::hasPages. It reports whether there is anywhere to
// go from here, in either direction.
func (p *CursorPaginator[T]) HasPages() bool { return !p.OnFirstPage() || p.HasMorePages() }

// OnFirstPage is CursorPaginator::onFirstPage. It reports whether this page
// starts the result set: either it was
// read without a cursor, or it was read backwards and found no row beyond it.
func (p *CursorPaginator[T]) OnFirstPage() bool {
	return p.cursor == nil || (p.cursor.PointsToPreviousItems() && !p.hasMore)
}

// OnLastPage is CursorPaginator::onLastPage. It reports whether this page ends
// the result set.
func (p *CursorPaginator[T]) OnLastPage() bool { return !p.HasMorePages() }

// GetCursorForItem is AbstractCursorPaginator::getCursorForItem. It is
// the cursor that reads the page beginning, or ending, at the given row.
//
// isNext says which side of the row the next query reads: true is the ordinary
// forward page, false the backward one.
//
// PHP throws when it cannot read the ordering columns off the item; this
// answers the error, and answers one on a paginator with no key function --
// which is what ThroughCursor leaves behind, the key having been written
// against the row type that was mapped away.
func (p *CursorPaginator[T]) GetCursorForItem(item T, isNext bool) (Cursor, error) {
	parameters, err := p.GetParametersForItem(item)
	if err != nil {
		return Cursor{}, err
	}
	return NewCursor(parameters, isNext), nil
}

// GetParametersForItem is AbstractCursorPaginator::getParametersForItem.
// It is the value the row has in each column the query orders by, keyed by
// column name, which is what a [Cursor] is made of.
//
// PHP reads them off the model by attribute name, walking pivot relations for a
// dotted one. Here the key function CursorPaginate was built with is what reads
// them, because a Go row has no attribute bag to look a name up in.
func (p *CursorPaginator[T]) GetParametersForItem(item T) (map[string]string, error) {
	if p.key == nil {
		return nil, errors.New("pagination: this paginator has no key function, so it cannot build a cursor for an item")
	}
	return p.key(item), nil
}

// GetCursorName is AbstractCursorPaginator::getCursorName. It returns
// the query parameter the encoded cursor is written into.
func (p *CursorPaginator[T]) GetCursorName() string { return p.options.CursorName }

// SetCursorName is AbstractCursorPaginator::setCursorName. It sets the
// query parameter the encoded cursor is written into, which is how two cursor
// paginators appear on one screen without moving each other.
func (p *CursorPaginator[T]) SetCursorName(name string) *CursorPaginator[T] {
	if name == "" {
		name = DefaultCursorName
	}
	p.options.CursorName = name
	return p
}

// URL is AbstractCursorPaginator::url. It returns the address that reads the
// given cursor. A nil cursor is the
// first page, whose URL carries no cursor parameter at all.
func (p *CursorPaginator[T]) URL(cursor *Cursor) string {
	if cursor == nil {
		return p.options.url(p.options.CursorName, "")
	}
	return p.options.url(p.options.CursorName, cursor.Encode())
}

// PreviousPageURL is AbstractCursorPaginator::previousPageUrl. It returns the
// address of the page before this one, and the empty string when there is none.
func (p *CursorPaginator[T]) PreviousPageURL() string {
	if p.previous == nil {
		return ""
	}
	return p.URL(p.previous)
}

// NextPageURL is AbstractCursorPaginator::nextPageUrl. It returns the address of
// the page after this one, and the empty string when there is none.
func (p *CursorPaginator[T]) NextPageURL() string {
	if p.next == nil {
		return ""
	}
	return p.URL(p.next)
}

// ThroughCursor is AbstractCursorPaginator::through on a CursorPaginator: the
// same page with every item passed through f.
//
// The cursors were computed when the page was built, from the rows as they came
// out of the database, so mapping the items cannot invalidate them. The key
// function does not come along: it was written against the row type that has
// just been mapped away, and GetCursorForItem on the result says so rather than
// keying the wrong row. See Through for why this is a function and why there
// are three of them.
func ThroughCursor[A, B any](p *CursorPaginator[A], f func(A) B) *CursorPaginator[B] {
	items := make([]B, len(p.items))
	for i, item := range p.items {
		items[i] = f(item)
	}
	return &CursorPaginator[B]{
		items:    items,
		perPage:  p.perPage,
		cursor:   p.cursor,
		hasMore:  p.hasMore,
		previous: p.previous,
		next:     p.next,
		options:  p.options,
	}
}

// IsEmpty is AbstractCursorPaginator::isEmpty. It reports whether this page
// holds no rows.
func (p *CursorPaginator[T]) IsEmpty() bool { return len(p.items) == 0 }

// IsNotEmpty is AbstractCursorPaginator::isNotEmpty. It reports whether this
// page holds any rows.
func (p *CursorPaginator[T]) IsNotEmpty() bool { return len(p.items) > 0 }

// GetIterator is AbstractCursorPaginator::getIterator. It ranges over the rows
// of this page with their offsets.
//
// PHP returns an ArrayIterator because IteratorAggregate demands one; Go's
// range-over-func is the same thing under another name.
func (p *CursorPaginator[T]) GetIterator() iter.Seq2[int, T] { return seq2(p.items) }

// GetCollection is AbstractCursorPaginator::getCollection. It returns the rows
// this page holds.
//
// Illuminate distinguishes the Collection from the plain array items() returns;
// here both are the same slice, and both names are kept because both are called.
func (p *CursorPaginator[T]) GetCollection() []T { return p.items }

// SetCollection is AbstractCursorPaginator::setCollection. It replaces the rows
// this page holds, leaving the cursors alone.
//
// The cursors were computed when the page was built, from the rows as the
// database returned them, so replacing the items cannot invalidate them.
func (p *CursorPaginator[T]) SetCollection(items []T) *CursorPaginator[T] {
	p.items = items
	return p
}

// GetOptions is AbstractCursorPaginator::getOptions. It returns the options this
// page builds its URLs from, with the defaults already applied.
func (p *CursorPaginator[T]) GetOptions() Options { return p.options }

// Path is AbstractCursorPaginator::path. It returns the base path the cursor
// links are built on.
func (p *CursorPaginator[T]) Path() string { return p.options.Path }

// SetPath is AbstractCursorPaginator::setPath. It sets the base path the cursor
// links are built on.
func (p *CursorPaginator[T]) SetPath(path string) *CursorPaginator[T] {
	p.options.Path = path
	return p
}

// WithPath is AbstractCursorPaginator::withPath, which in Illuminate is setPath
// under the name the fluent calls read better with.
func (p *CursorPaginator[T]) WithPath(path string) *CursorPaginator[T] {
	return p.SetPath(path)
}

// Fragment is AbstractCursorPaginator::fragment. It sets the fragment appended
// after a "#", so a cursor link can land on the table rather than the top of the
// document.
//
// PHP's fragment() is a getter when called with no argument and a setter
// otherwise, on one name. Go has neither optional parameters nor a union return,
// so this is the setter -- the form the fluent calls use -- and the fragment is
// read back through GetOptions. An empty string clears it.
func (p *CursorPaginator[T]) Fragment(fragment string) *CursorPaginator[T] {
	p.options.Fragment = fragment
	return p
}

// Appends is AbstractCursorPaginator::appends. It carries extra query string
// values onto every generated URL, which is what keeps a filter or a sort
// selected while the reader walks the pages.
//
// PHP declares the first parameter as array|string|null. Go has no union type,
// so key is a string naming one parameter -- with its value as the second
// argument -- or a map[string]string, a url.Values or a map[string][]string
// naming several. Anything else, nil included, is ignored, exactly as PHP
// ignores null.
//
// The cursor parameter is never appended: the paginator writes that itself,
// which is the `if ($key !== $this->cursorName)` in addQuery().
func (p *CursorPaginator[T]) Appends(key any, value ...string) *CursorPaginator[T] {
	appendQuery(&p.options, p.options.CursorName, key, value)
	return p
}

// WithQueryString is AbstractCursorPaginator::withQueryString. It carries every
// parameter of the current request onto the generated URLs.
//
// Illuminate reads them from a static closure a service provider installs; there
// is no container here (ADR 0001), so they are passed in -- normally Query of
// the request URL. The cursor parameter is dropped.
func (p *CursorPaginator[T]) WithQueryString(query url.Values) *CursorPaginator[T] {
	mergeQuery(&p.options, p.options.CursorName, query)
	return p
}

// ToArray is CursorPaginator::toArray. It is the payload Illuminate serialises a
// cursor page to, key for key.
//
// next_cursor and prev_cursor are the encoded cursors and are null when there is
// none, which is PHP's `$this->nextCursor()?->encode()`.
func (p *CursorPaginator[T]) ToArray() map[string]any {
	encode := func(c *Cursor) any {
		if c == nil {
			return nil
		}
		return c.Encode()
	}
	return map[string]any{
		"data":          p.items,
		"path":          p.Path(),
		"per_page":      p.perPage,
		"next_cursor":   encode(p.next),
		"next_page_url": nullable(p.NextPageURL()),
		"prev_cursor":   encode(p.previous),
		"prev_page_url": nullable(p.PreviousPageURL()),
	}
}

// MarshalJSON is CursorPaginator::jsonSerialize, which is PHP's
// JsonSerializable; Go's name for the same contract is json.Marshaler. It makes
// the paginator itself encodable, so a handler can hand one to a JSON encoder.
func (p *CursorPaginator[T]) MarshalJSON() ([]byte, error) { return json.Marshal(p.ToArray()) }

// ToJSON is CursorPaginator::toJson. PHP takes json_encode flags and returns a
// string; this returns the bytes and the error json.Marshal reports, which is
// where the exception went.
func (p *CursorPaginator[T]) ToJSON() ([]byte, error) { return json.Marshal(p.ToArray()) }

// ToPrettyJSON is CursorPaginator::toPrettyJson. It is ToJSON indented four
// spaces, as PHP's JSON_PRETTY_PRINT indents.
func (p *CursorPaginator[T]) ToPrettyJSON() ([]byte, error) { return prettyJSON(p.ToArray()) }
