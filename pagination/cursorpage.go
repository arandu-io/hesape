package pagination

import (
	"encoding/json"
	"net/url"
)

// CursorPage is the arithmetic of one page of a keyset walk: how many rows are
// on one side of a Cursor, and the cursors that reach the pages either side of
// it.
//
// It is the one page that holds still. There is no total and no page number --
// naming a page by number means counting rows, and counting rows is the cost
// keyset paging exists to avoid. What it offers is previous and next, both of
// them exact.
//
// It holds no rows. The rows are whatever the query that read them returned,
// handed to the caller beside the page, so this type is the same one for every
// kind of row and is compiled once rather than once per row type.
//
// Build one with NewCursorPage.
type CursorPage struct {
	count    int
	perPage  int
	cursor   *Cursor
	hasMore  bool
	reversed bool
	previous *Cursor
	next     *Cursor
	options  Options
}

// NewCursorPage returns the page a keyset query read rows for.
//
// The query reads perPage+1 rows from the boundary named by cursor -- nil for
// the first page -- and read is how many came back. The extra row answers "is
// there another page", and the page holds at most perPage rows: Count says how
// many.
//
// A cursor whose PointsToNextItems is false was read backwards, so its rows
// arrive in reverse order and Reversed reports true. The rows on the page, in
// reading order, are therefore the first Count rows as they were read, turned
// around when Reversed says so -- the caller arranges them, because the page
// does not hold them. That is also why the probe row is dropped before the
// reversal: on a backward page the extra row is the one furthest from the
// boundary, which is the first one the reader would have seen.
//
// key is how a row becomes a cursor: given the index of a row as it was read, it
// returns the value of each column the query orders by, keyed by column name. It
// is called for the two rows at the edges of the page and for no other. It must
// name the same columns as the ORDER BY, and it must format them so that parsing
// them back yields the same value -- a truncated timestamp walks past every row
// that shares it. It is required, and NewCursorPage panics on a nil key: a page
// that quietly produced no cursors would render a pager with no way forward and
// no error to explain it.
//
// opts.Signer is required for the same reason and panics for a harder one: the
// cursor in a link is a boundary row the client sends back, so a page whose
// links carry an unsigned one is a page whose reader chooses which rows the next
// query starts at.
func NewCursorPage(read, perPage int, cursor *Cursor, key func(i int) map[string]string, opts Options) *CursorPage {
	if key == nil {
		panic("pagination: NewCursorPage needs a key function to build cursors from rows")
	}
	if opts.Signer == nil {
		panic("pagination: NewCursorPage needs a signer to write its cursors with")
	}
	if perPage < 1 {
		perPage = 1
	}
	if read < 0 {
		read = 0
	}

	hasMore := read > perPage
	count := read
	if hasMore {
		count = perPage
	}
	backwards := cursor != nil && !cursor.PointsToNextItems()

	p := &CursorPage{
		count:    count,
		perPage:  perPage,
		cursor:   cursor,
		hasMore:  hasMore,
		reversed: backwards,
		options:  opts.normalize(),
	}
	if count > 0 {
		// The first row in reading order is the last one kept when the rows
		// arrived backwards, and the first one read otherwise.
		first, last := 0, count-1
		if backwards {
			first, last = count-1, 0
		}

		// A backward page that found no extra row has reached the start of the
		// result set, so there is nothing before it; every other page has a
		// row to point back at. Symmetrically forward, with hasMore.
		if !(cursor == nil || (backwards && !hasMore)) {
			previous := NewCursor(key(first), false)
			p.previous = &previous
		}
		if hasMore || backwards {
			next := NewCursor(key(last), true)
			p.next = &next
		}
	}
	return p
}

// Count returns how many rows this page holds, with the probe row already left
// out.
func (p *CursorPage) Count() int { return p.count }

// Reversed reports whether the rows were read backwards, and so have to be
// turned around to be in reading order. See NewCursorPage.
func (p *CursorPage) Reversed() bool { return p.reversed }

// IsEmpty reports whether this page holds no rows.
func (p *CursorPage) IsEmpty() bool { return p.count == 0 }

// IsNotEmpty reports whether this page holds any rows.
func (p *CursorPage) IsNotEmpty() bool { return p.count > 0 }

// PerPage returns the page size the page was built with.
func (p *CursorPage) PerPage() int { return p.perPage }

// Cursor returns the cursor this page was read from, and nil on the first
// page.
func (p *CursorPage) Cursor() *Cursor { return p.cursor }

// PreviousCursor returns the cursor that reads the page before this one, and
// nil when there is none.
func (p *CursorPage) PreviousCursor() *Cursor { return p.previous }

// NextCursor returns the cursor that reads the page after this one, and nil
// when there is none.
func (p *CursorPage) NextCursor() *Cursor { return p.next }

// HasMorePages reports whether a page follows this one.
func (p *CursorPage) HasMorePages() bool { return p.next != nil }

// HasPages reports whether there is anywhere to go from here, in either
// direction.
func (p *CursorPage) HasPages() bool { return !p.OnFirstPage() || p.HasMorePages() }

// OnFirstPage reports whether this page starts the result set: either it was
// read without a cursor, or it was read backwards and found no row beyond it.
func (p *CursorPage) OnFirstPage() bool {
	return p.cursor == nil || (p.cursor.PointsToPreviousItems() && !p.hasMore)
}

// OnLastPage reports whether this page ends the result set.
func (p *CursorPage) OnLastPage() bool { return !p.HasMorePages() }

// GetCursorName returns the query parameter the encoded cursor is written
// into.
func (p *CursorPage) GetCursorName() string { return p.options.CursorName }

// SetCursorName sets the query parameter the encoded cursor is written into,
// which is how two cursor pagers appear on one screen without moving each
// other.
func (p *CursorPage) SetCursorName(name string) *CursorPage {
	if name == "" {
		name = DefaultCursorName
	}
	p.options.CursorName = name
	return p
}

// URL returns the address that reads the given cursor. A nil cursor is the
// first page, whose URL carries no cursor parameter at all.
//
// A cursor [CursorSigner.Encode] has no token for is the empty string, not the
// first page: the two differ by one query parameter and by everything else. A
// link back to the top of a list, offered as the next page, is a loop nobody
// reading it can see.
func (p *CursorPage) URL(cursor *Cursor) string {
	if cursor == nil {
		return p.options.url(p.options.CursorName, "")
	}
	encoded := p.options.Signer.Encode(*cursor)
	if encoded == "" {
		return ""
	}
	return p.options.url(p.options.CursorName, encoded)
}

// PreviousPageURL returns the address of the page before this one, and the
// empty string when there is none.
func (p *CursorPage) PreviousPageURL() string {
	if p.previous == nil {
		return ""
	}
	return p.URL(p.previous)
}

// NextPageURL returns the address of the page after this one, and the empty
// string when there is none.
func (p *CursorPage) NextPageURL() string {
	if p.next == nil {
		return ""
	}
	return p.URL(p.next)
}

// GetOptions returns the options this page builds its URLs from, with the
// defaults already applied.
func (p *CursorPage) GetOptions() Options { return p.options }

// Path returns the base path the cursor links are built on.
func (p *CursorPage) Path() string { return p.options.Path }

// SetPath sets the base path the cursor links are built on.
func (p *CursorPage) SetPath(path string) *CursorPage {
	p.options.Path = path
	return p
}

// WithPath sets the base address every page link is built on.
func (p *CursorPage) WithPath(path string) *CursorPage { return p.SetPath(path) }

// Fragment sets the fragment appended after a "#", so a cursor link can land
// on the table rather than the top of the document.
//
// It is the setter only -- the form the fluent calls use -- and the fragment is
// read back through GetOptions. An empty string clears it.
func (p *CursorPage) Fragment(fragment string) *CursorPage {
	p.options.Fragment = fragment
	return p
}

// Appends carries extra query string values onto every generated URL, which is
// what keeps a filter or a sort selected while the reader walks the pages.
//
// key is a string naming one parameter -- with its value as the second argument
// -- or a map[string]string, a url.Values or a map[string][]string naming
// several. Anything else, nil included, is ignored.
//
// The cursor parameter is never appended: the page writes that itself.
func (p *CursorPage) Appends(key any, value ...string) *CursorPage {
	appendQuery(&p.options, p.options.CursorName, key, value)
	return p
}

// WithQueryString carries every parameter of the current request onto the
// generated URLs.
//
// The parameters are passed in -- normally Query of the request URL. The cursor
// parameter is dropped.
func (p *CursorPage) WithQueryString(query url.Values) *CursorPage {
	mergeQuery(&p.options, p.options.CursorName, query)
	return p
}

// ToArray is the payload a cursor page serialises to, with data -- the rows the
// page was read for, in reading order -- under "data".
//
// next_cursor and prev_cursor are the encoded cursors, and are null when there
// is none -- which includes a cursor [CursorSigner.Encode] has no token for.
// Null and the empty string mean the same thing to whoever reads this, and only
// one of them means it in every client.
func (p *CursorPage) ToArray(data any) map[string]any {
	out := p.meta()
	out["data"] = data
	return out
}

// meta is the payload without the rows.
func (p *CursorPage) meta() map[string]any {
	encode := func(c *Cursor) any {
		if c == nil {
			return nil
		}
		return nullable(p.options.Signer.Encode(*c))
	}
	return map[string]any{
		"path":          p.Path(),
		"per_page":      p.perPage,
		"next_cursor":   encode(p.next),
		"next_page_url": nullable(p.NextPageURL()),
		"prev_cursor":   encode(p.previous),
		"prev_page_url": nullable(p.PreviousPageURL()),
	}
}

// MarshalJSON encodes the page without the rows, which it does not hold: a
// response that carries both puts the rows beside it, or calls ToJSON with them.
func (p *CursorPage) MarshalJSON() ([]byte, error) { return json.Marshal(p.meta()) }

// ToJSON returns the payload with data under "data", as bytes, with the error
// json.Marshal reports.
func (p *CursorPage) ToJSON(data any) ([]byte, error) { return json.Marshal(p.ToArray(data)) }

// ToPrettyJSON is ToJSON indented four spaces.
func (p *CursorPage) ToPrettyJSON(data any) ([]byte, error) { return prettyJSON(p.ToArray(data)) }
