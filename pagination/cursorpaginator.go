package pagination

import "errors"

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

// CursorPaginate returns the page holding items.
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

// Items returns the rows of this page, in reading order, with the probe row
// already removed.
func (p *CursorPaginator[T]) Items() []T { return p.items }

// Count returns how many rows this page holds.
func (p *CursorPaginator[T]) Count() int { return len(p.items) }

// PerPage returns the page size the paginator was built with.
func (p *CursorPaginator[T]) PerPage() int { return p.perPage }

// Cursor returns the cursor this page was read from, and nil on the first page.
func (p *CursorPaginator[T]) Cursor() *Cursor { return p.cursor }

// PreviousCursor returns the cursor that reads the page before this one, and
// nil when there is none.
func (p *CursorPaginator[T]) PreviousCursor() *Cursor { return p.previous }

// NextCursor returns the cursor that reads the page after this one, and nil
// when there is none.
func (p *CursorPaginator[T]) NextCursor() *Cursor { return p.next }

// HasMorePages reports whether a page follows this one.
func (p *CursorPaginator[T]) HasMorePages() bool { return p.next != nil }

// HasPages reports whether there is anywhere to go from here, in either
// direction.
func (p *CursorPaginator[T]) HasPages() bool { return !p.OnFirstPage() || p.HasMorePages() }

// OnFirstPage reports whether this page starts the result set: either it was
// read without a cursor, or it was read backwards and found no row beyond it.
func (p *CursorPaginator[T]) OnFirstPage() bool {
	return p.cursor == nil || (p.cursor.PointsToPreviousItems() && !p.hasMore)
}

// OnLastPage reports whether this page ends the result set.
func (p *CursorPaginator[T]) OnLastPage() bool { return !p.HasMorePages() }

// GetCursorForItem answers AbstractCursorPaginator::getCursorForItem(). It is
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

// GetParametersForItem answers AbstractCursorPaginator::getParametersForItem().
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

// GetCursorName answers AbstractCursorPaginator::getCursorName(). It returns
// the query parameter the encoded cursor is written into.
func (p *CursorPaginator[T]) GetCursorName() string { return p.options.CursorName }

// SetCursorName answers AbstractCursorPaginator::setCursorName(). It sets the
// query parameter the encoded cursor is written into, which is how two cursor
// paginators appear on one screen without moving each other.
func (p *CursorPaginator[T]) SetCursorName(name string) *CursorPaginator[T] {
	if name == "" {
		name = DefaultCursorName
	}
	p.options.CursorName = name
	return p
}

// URL returns the address that reads the given cursor. A nil cursor is the
// first page, whose URL carries no cursor parameter at all.
func (p *CursorPaginator[T]) URL(cursor *Cursor) string {
	if cursor == nil {
		return p.options.url(p.options.CursorName, "")
	}
	return p.options.url(p.options.CursorName, cursor.Encode())
}

// PreviousPageURL returns the address of the page before this one, and the
// empty string when there is none.
func (p *CursorPaginator[T]) PreviousPageURL() string {
	if p.previous == nil {
		return ""
	}
	return p.URL(p.previous)
}

// NextPageURL returns the address of the page after this one, and the empty
// string when there is none.
func (p *CursorPaginator[T]) NextPageURL() string {
	if p.next == nil {
		return ""
	}
	return p.URL(p.next)
}

// ThroughCursor returns the same page with every item passed through f.
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
