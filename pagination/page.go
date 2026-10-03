package pagination

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// Page is the arithmetic of one page of a result set whose size is not known.
//
// There is no total, so there is no last page and no numbered window: it can
// offer previous and next, and that is the whole of it. What it buys is the
// COUNT query it does not run, which on a large table is the reason to reach for
// it.
//
// It holds no rows. The rows are whatever the query that read them returned,
// handed to the caller beside the page, so this type is the same one for every
// kind of row and is compiled once rather than once per row type.
//
// Build one with NewPage.
type Page struct {
	count       int
	perPage     int
	currentPage int
	hasMore     bool
	onEachSide  int
	options     Options
}

// NewPage returns the page a query read rows for.
//
// read is how many rows came back from a query that asked for perPage+1: the
// extra row is how "is there a next page" is answered without counting, so the
// page holds at most perPage of them, and Count says how many. The caller keeps
// the first Count rows and drops the rest. Reading exactly perPage rows is not a
// bug, it just means the page reports no next page -- which is right whenever
// the result set ends there, and wrong by one page when it does not.
//
// perPage below one is read as one, currentPage below one as one, and read
// below zero as zero.
func NewPage(read, perPage, currentPage int, opts Options) *Page {
	if perPage < 1 {
		perPage = 1
	}
	if currentPage < 1 {
		currentPage = 1
	}
	if read < 0 {
		read = 0
	}

	hasMore := read > perPage
	if hasMore {
		read = perPage
	}

	normalized := opts.normalize()
	return &Page{
		count:       read,
		perPage:     perPage,
		currentPage: currentPage,
		hasMore:     hasMore,
		onEachSide:  normalized.OnEachSide,
		options:     normalized,
	}
}

// Count returns how many rows this page holds, with the probe row already left
// out.
func (p *Page) Count() int { return p.count }

// IsEmpty reports whether this page holds no rows.
func (p *Page) IsEmpty() bool { return p.count == 0 }

// IsNotEmpty reports whether this page holds any rows.
func (p *Page) IsNotEmpty() bool { return p.count > 0 }

// GetOptions returns the options this page builds its URLs from, with the
// defaults already applied.
func (p *Page) GetOptions() Options { return p.options }

// PerPage returns the page size the page was built with.
func (p *Page) PerPage() int { return p.perPage }

// CurrentPage returns the page being read, counting from one.
func (p *Page) CurrentPage() int { return p.currentPage }

// FirstItem returns the one-based index, in the whole result set, of the first
// row on this page, and zero when the page is empty.
func (p *Page) FirstItem() int {
	if p.count == 0 {
		return 0
	}
	return (p.currentPage-1)*p.perPage + 1
}

// LastItem returns the one-based index, in the whole result set, of the last
// row on this page, and zero when the page is empty.
func (p *Page) LastItem() int {
	if p.count == 0 {
		return 0
	}
	return p.FirstItem() + p.count - 1
}

// HasMorePages reports whether a page follows this one, which is what the
// probe row answered.
func (p *Page) HasMorePages() bool { return p.hasMore }

// HasMorePagesWhen overrides that answer, for a caller that knows something
// the probe row does not.
func (p *Page) HasMorePagesWhen(hasMore bool) *Page {
	p.hasMore = hasMore
	return p
}

// HasPages reports whether there is anywhere to go from here, in either
// direction.
func (p *Page) HasPages() bool { return p.currentPage != 1 || p.hasMore }

// OnFirstPage reports whether this is page one.
func (p *Page) OnFirstPage() bool { return p.currentPage <= 1 }

// OnLastPage reports whether no page follows this one.
func (p *Page) OnLastPage() bool { return !p.hasMore }

// URL returns the address of the given page; a page below one is read as one.
func (p *Page) URL(page int) string {
	if page < 1 {
		page = 1
	}
	return p.options.url(p.options.PageName, strconv.Itoa(page))
}

// GetURLRange returns the address of every page from start to end inclusive,
// keyed by page number.
//
// A simple page has no last page to bound the range with, so the caller
// supplies both ends.
func (p *Page) GetURLRange(start, end int) map[int]string {
	pages := pageRange(start, end)
	if len(pages) == 0 {
		return nil
	}
	out := make(map[int]string, len(pages))
	for _, page := range pages {
		out[page] = p.URL(page)
	}
	return out
}

// PreviousPageURL returns the address of the page before this one, and the
// empty string on page one.
func (p *Page) PreviousPageURL() string {
	if p.currentPage <= 1 {
		return ""
	}
	return p.URL(p.currentPage - 1)
}

// NextPageURL returns the address of the page after this one, and the empty
// string when no row was left over to prove there is one.
func (p *Page) NextPageURL() string {
	if !p.hasMore {
		return ""
	}
	return p.URL(p.currentPage + 1)
}

// Path returns the base path the page links are built on.
func (p *Page) Path() string { return p.options.Path }

// SetPath sets the base path the page links are built on.
func (p *Page) SetPath(path string) *Page {
	p.options.Path = path
	return p
}

// WithPath sets the base address every page link is built on.
func (p *Page) WithPath(path string) *Page { return p.SetPath(path) }

// GetPageName returns the query parameter the page number is written into.
func (p *Page) GetPageName() string { return p.options.PageName }

// SetPageName sets the query parameter the page number is written into.
func (p *Page) SetPageName(name string) *Page {
	p.options.PageName = name
	return p
}

// OnEachSide sets how many numbered links sit either side of the current page.
//
// A simple page renders no numbered window, so nothing here reads it. It is
// kept so that a caller swapping a simple page for a length-aware one does not
// have to delete the call.
func (p *Page) OnEachSide(count int) *Page {
	if count < 0 {
		count = 0
	}
	p.onEachSide = count
	p.options.OnEachSide = count
	return p
}

// Fragment sets the fragment appended after a "#". See
// LengthAwarePage.Fragment for why this is the setter alone.
func (p *Page) Fragment(fragment string) *Page {
	p.options.Fragment = fragment
	return p
}

// Appends carries extra query string values onto every generated URL. See
// LengthAwarePage.Appends for the forms key takes.
func (p *Page) Appends(key any, value ...string) *Page {
	appendQuery(&p.options, p.options.PageName, key, value)
	return p
}

// WithQueryString carries every parameter of the current request onto the
// generated URLs; there is no static resolver here, so they are passed in.
func (p *Page) WithQueryString(query url.Values) *Page {
	mergeQuery(&p.options, p.options.PageName, query)
	return p
}

// ToArray is the payload a simple page serialises to, with data -- the rows the
// page was read for -- under "data". There is no total and no last page, and
// there is a current_page_url the length-aware payload does not carry.
func (p *Page) ToArray(data any) map[string]any {
	out := p.meta()
	out["data"] = data
	return out
}

// meta is the payload without the rows.
func (p *Page) meta() map[string]any {
	return map[string]any{
		"current_page":     p.currentPage,
		"current_page_url": p.URL(p.currentPage),
		"first_page_url":   p.URL(1),
		"from":             nullable(p.FirstItem()),
		"next_page_url":    nullable(p.NextPageURL()),
		"path":             p.Path(),
		"per_page":         p.perPage,
		"prev_page_url":    nullable(p.PreviousPageURL()),
		"to":               nullable(p.LastItem()),
	}
}

// MarshalJSON encodes the page without the rows, which it does not hold: a
// response that carries both puts the rows beside it, or calls ToJSON with them.
func (p *Page) MarshalJSON() ([]byte, error) { return json.Marshal(p.meta()) }

// ToJSON returns the payload with data under "data", as bytes; bad UTF-8 is the
// error.
func (p *Page) ToJSON(data any) ([]byte, error) { return json.Marshal(p.ToArray(data)) }

// ToPrettyJSON is ToJSON indented four spaces.
func (p *Page) ToPrettyJSON(data any) ([]byte, error) { return prettyJSON(p.ToArray(data)) }
