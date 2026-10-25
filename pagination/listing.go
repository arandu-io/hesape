package pagination

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The parameter names a listing reads when its [ListingNames] leaves one empty.
const (
	// DefaultSearchName is the query parameter holding the search text.
	DefaultSearchName = "q"

	// DefaultSortName is the query parameter holding the public key of the
	// column the rows are ordered by.
	DefaultSortName = "sort"

	// DefaultDirName is the query parameter holding the sort direction.
	DefaultDirName = "dir"

	// DefaultPerPageName is the query parameter holding the page size.
	DefaultPerPageName = "per_page"

	// DefaultHideName is the query parameter holding the keys of the columns
	// the reader hid. It is repeated, once per hidden column.
	DefaultHideName = "hide"
)

// DefaultPageSizes are the page sizes a listing offers when its spec declares
// none.
var DefaultPageSizes = []int{10, 20, 25, 30, 40, 50}

// DefaultTieBreak is the column a listing orders by after its sort when its
// spec names none.
const DefaultTieBreak = "id"

// maxSearchRunes bounds the search text. A longer one is cut: nobody types a
// filter that long, and the text becomes a LIKE pattern bound to every search
// column.
const maxSearchRunes = 200

// maxListingPage bounds the page number a listing accepts, so that the offset a
// page turns into cannot overflow.
const maxListingPage = 1_000_000

// SortDirection is the way a sort runs.
type SortDirection string

// The two directions. Nothing else is a direction: a value that is neither
// reads as the default.
const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

// Flip returns the opposite direction.
func (d SortDirection) Flip() SortDirection {
	if d == SortDesc {
		return SortAsc
	}
	return SortDesc
}

// ListingNames are the query parameters a listing reads and writes. An empty
// field means the default; the name of a facet's parameter is on the facet.
type ListingNames struct {
	Search  string
	Sort    string
	Dir     string
	Page    string
	PerPage string
	Hide    string
}

// FacetSpec declares one filter of a listing: a column and the values a reader
// may select from it.
//
// The values are the whole whitelist. A value the request carries that is not
// here is dropped, so the column is only ever compared against what the
// application listed. The facet's column is compared with equality, so it is
// meant for a column that holds a short, enumerable text: a status, a priority,
// a kind.
type FacetSpec struct {
	// Key is the facet's public name. It is the facet's query parameter unless
	// Param says otherwise, and it is what the state is asked for it by.
	Key string

	// Param is the query parameter the facet travels in. Empty means Key.
	Param string

	// Column is the column the facet filters, never taken from the request.
	Column string

	// Values are the values a reader may select, in the order they are listed
	// and written.
	Values []string
}

// ListingSpec is a listing declared: what may be searched, sorted, filtered,
// paged and hidden, and under which names the request carries it.
//
// It is a plain value an application writes once, beside the model it lists.
// [NewListing] checks it and returns the [Listing] that reads requests; nothing
// in the request can name a column the spec did not.
type ListingSpec struct {
	// Search are the columns the search text is looked for in, with "contains"
	// and without regard to case. They hold text.
	Search []string

	// Sorts maps the public key of a sortable column to the column it orders
	// by. A sort the request names that is not a key here is ignored.
	Sorts map[string]string

	// DefaultSort is the key the rows are ordered by when the request names no
	// valid one. Empty means the rows are ordered by the tie-break alone.
	DefaultSort string

	// DefaultDir is the direction of the default sort. Empty means ascending.
	// Any other sort runs ascending until the reader flips it.
	DefaultDir SortDirection

	// TieBreak is the column the rows are ordered by after the sort, so that
	// two rows with the same sorted value always land in the same order and a
	// row is neither skipped nor repeated between pages. It should be unique.
	// Empty means [DefaultTieBreak].
	TieBreak string

	// Facets are the filters the listing offers.
	Facets []FacetSpec

	// PageSizes are the page sizes a reader may choose. Empty means
	// [DefaultPageSizes].
	PageSizes []int

	// DefaultPageSize is the page size when the request names none. It must be
	// one of PageSizes. Zero means 10 when 10 is offered, and the first size
	// otherwise.
	DefaultPageSize int

	// Hideable are the keys of the columns a reader may hide.
	Hideable []string

	// Names override the query parameters.
	Names ListingNames
}

// Listing is a [ListingSpec] that has been checked: the value that turns the
// query string of a request into a [ListingState].
//
// It is immutable and safe for concurrent use. Build it once, when the service
// that lists is built.
type Listing struct {
	search      []string
	sorts       map[string]string
	defaultSort string
	defaultDir  SortDirection
	tieBreak    string
	facets      []FacetSpec
	pageSizes   []int
	defaultSize int
	hideable    []string
	names       ListingNames
}

// NewListing checks spec and returns the listing it declares.
//
// It reports every problem it finds, joined: a column left empty, a default
// sort that is not a sort, a default page size that is not offered, a facet
// without values, and two things sharing one query parameter. A spec that fails
// here is a mistake in the code that wrote it, found when the service is built
// rather than on the first request.
func NewListing(spec ListingSpec) (*Listing, error) {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	l := &Listing{
		tieBreak:   spec.TieBreak,
		defaultDir: spec.DefaultDir,
		names:      spec.Names,
	}
	if l.tieBreak == "" {
		l.tieBreak = DefaultTieBreak
	}
	switch l.defaultDir {
	case "":
		l.defaultDir = SortAsc
	case SortAsc, SortDesc:
	default:
		fail("pagination: the default direction %q is neither %q nor %q", l.defaultDir, SortAsc, SortDesc)
		l.defaultDir = SortAsc
	}

	for _, column := range spec.Search {
		if strings.TrimSpace(column) == "" {
			fail("pagination: a search column is empty")
			continue
		}
		if !slices.Contains(l.search, column) {
			l.search = append(l.search, column)
		}
	}

	l.sorts = make(map[string]string, len(spec.Sorts))
	for key, column := range spec.Sorts {
		if key == "" || strings.TrimSpace(column) == "" {
			fail("pagination: the sort %q maps to the column %q, and neither may be empty", key, column)
			continue
		}
		l.sorts[key] = column
	}
	if spec.DefaultSort != "" {
		if _, ok := l.sorts[spec.DefaultSort]; !ok {
			fail("pagination: the default sort %q is not one of the sorts", spec.DefaultSort)
		} else {
			l.defaultSort = spec.DefaultSort
		}
	}

	l.names.Search = nameOr(l.names.Search, DefaultSearchName)
	l.names.Sort = nameOr(l.names.Sort, DefaultSortName)
	l.names.Dir = nameOr(l.names.Dir, DefaultDirName)
	l.names.Page = nameOr(l.names.Page, DefaultPageName)
	l.names.PerPage = nameOr(l.names.PerPage, DefaultPerPageName)
	l.names.Hide = nameOr(l.names.Hide, DefaultHideName)

	taken := map[string]string{}
	claim := func(name, owner string) {
		if previous, ok := taken[name]; ok {
			fail("pagination: the query parameter %q is used by both %s and %s", name, previous, owner)
			return
		}
		taken[name] = owner
	}
	claim(l.names.Search, "the search")
	claim(l.names.Sort, "the sort")
	claim(l.names.Dir, "the direction")
	claim(l.names.Page, "the page")
	claim(l.names.PerPage, "the page size")
	claim(l.names.Hide, "the hidden columns")

	seenKeys := map[string]bool{}
	for _, facet := range spec.Facets {
		if facet.Key == "" || strings.TrimSpace(facet.Column) == "" {
			fail("pagination: the facet %q on the column %q needs a key and a column", facet.Key, facet.Column)
			continue
		}
		if seenKeys[facet.Key] {
			fail("pagination: the facet %q is declared twice", facet.Key)
			continue
		}
		seenKeys[facet.Key] = true
		if facet.Param == "" {
			facet.Param = facet.Key
		}
		claim(facet.Param, "the facet "+strconv.Quote(facet.Key))

		values := make([]string, 0, len(facet.Values))
		for _, value := range facet.Values {
			if value == "" {
				fail("pagination: the facet %q lists an empty value", facet.Key)
				continue
			}
			if !slices.Contains(values, value) {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			fail("pagination: the facet %q lists no values", facet.Key)
			continue
		}
		facet.Values = values
		l.facets = append(l.facets, facet)
	}

	sizes := spec.PageSizes
	if len(sizes) == 0 {
		sizes = DefaultPageSizes
	}
	for _, size := range sizes {
		switch {
		case size < 1:
			fail("pagination: the page size %d is not positive", size)
		case !slices.Contains(l.pageSizes, size):
			l.pageSizes = append(l.pageSizes, size)
		}
	}
	if len(l.pageSizes) == 0 {
		l.pageSizes = []int{10}
	}
	switch {
	case spec.DefaultPageSize != 0:
		if !slices.Contains(l.pageSizes, spec.DefaultPageSize) {
			fail("pagination: the default page size %d is not one of the page sizes %v", spec.DefaultPageSize, l.pageSizes)
		}
		l.defaultSize = spec.DefaultPageSize
	case slices.Contains(l.pageSizes, 10):
		l.defaultSize = 10
	default:
		l.defaultSize = l.pageSizes[0]
	}

	for _, key := range spec.Hideable {
		if key == "" {
			fail("pagination: a hideable column has an empty key")
			continue
		}
		if !slices.Contains(l.hideable, key) {
			l.hideable = append(l.hideable, key)
		}
	}

	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return l, nil
}

func nameOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// Names returns the query parameters the listing reads and writes, the defaults
// filled in.
func (l *Listing) Names() ListingNames { return l.names }

// SearchColumns returns the columns the search text is looked for in.
func (l *Listing) SearchColumns() []string { return slices.Clone(l.search) }

// PageSizes returns the page sizes a reader may choose, in the order they were
// declared.
func (l *Listing) PageSizes() []int { return slices.Clone(l.pageSizes) }

// DefaultPageSize returns the page size used when the request names none.
func (l *Listing) DefaultPageSize() int { return l.defaultSize }

// Hideable returns the keys of the columns a reader may hide.
func (l *Listing) Hideable() []string { return slices.Clone(l.hideable) }

// Facets returns the declared facets, in order, with their parameter and values
// filled in.
func (l *Listing) Facets() []FacetSpec {
	out := make([]FacetSpec, len(l.facets))
	for i, facet := range l.facets {
		facet.Values = slices.Clone(facet.Values)
		out[i] = facet
	}
	return out
}

// Facet returns the facet declared under key.
func (l *Listing) Facet(key string) (FacetSpec, bool) {
	for _, facet := range l.facets {
		if facet.Key == key {
			facet.Values = slices.Clone(facet.Values)
			return facet, true
		}
	}
	return FacetSpec{}, false
}

// Sortable reports whether key is a declared sort.
func (l *Listing) Sortable(key string) bool {
	_, ok := l.sorts[key]
	return ok
}

// Parse reads a listing state out of the query string of a request.
//
// Nothing in the query string is trusted and nothing in it fails the request:
//
//   - the search text is trimmed, cut at 200 characters and stripped of
//     control characters and bytes that are not UTF-8;
//   - the sort is the key in the query when it is a declared one, and the
//     default sort otherwise; the direction is "asc" or "desc", and otherwise
//     the default sort's direction on the default sort and ascending on any
//     other;
//   - a facet keeps the values that are listed for it, once each, in the order
//     the spec lists them;
//   - the page is a whole number from one up, and one for anything else;
//   - the page size is one of the declared sizes, and the default otherwise;
//   - the hidden columns are the hideable ones named, once each, in the order
//     the spec lists them.
func (l *Listing) Parse(values url.Values) ListingState {
	s := ListingState{listing: l, page: 1, perPage: l.defaultSize, sort: l.defaultSort}

	s.search = cleanSearch(values.Get(l.names.Search))

	if key := values.Get(l.names.Sort); l.Sortable(key) {
		s.sort = key
	}
	s.dir = l.directionFor(s.sort, SortDirection(values.Get(l.names.Dir)))

	for _, facet := range l.facets {
		if chosen := keepListed(values[facet.Param], facet.Values); len(chosen) > 0 {
			if s.facets == nil {
				s.facets = map[string][]string{}
			}
			s.facets[facet.Key] = chosen
		}
	}

	if page, err := strconv.Atoi(strings.TrimSpace(values.Get(l.names.Page))); err == nil && page >= 1 {
		s.page = min(page, maxListingPage)
	}
	if size, err := strconv.Atoi(strings.TrimSpace(values.Get(l.names.PerPage))); err == nil && slices.Contains(l.pageSizes, size) {
		s.perPage = size
	}
	s.hidden = keepListed(values[l.names.Hide], l.hideable)
	return s
}

// ParseURL is Parse over the query string of u. A nil URL reads as an empty one.
func (l *Listing) ParseURL(u *url.URL) ListingState {
	if u == nil {
		return l.Parse(nil)
	}
	return l.Parse(u.Query())
}

// directionFor resolves the direction a sort runs in: the one asked for when it
// is valid, and otherwise the default sort's own direction on the default sort
// and ascending on any other.
func (l *Listing) directionFor(sort string, asked SortDirection) SortDirection {
	switch asked {
	case SortAsc, SortDesc:
		return asked
	}
	return l.defaultDirectionOf(sort)
}

func (l *Listing) defaultDirectionOf(sort string) SortDirection {
	if sort != "" && sort == l.defaultSort {
		return l.defaultDir
	}
	return SortAsc
}

// cleanSearch makes the search text something a database accepts: valid UTF-8
// without control characters (Postgres refuses a NUL byte and an invalid
// sequence outright), trimmed, and no longer than maxSearchRunes.
func cleanSearch(text string) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxSearchRunes {
		text = strings.TrimSpace(string([]rune(text)[:maxSearchRunes]))
	}
	return text
}

// keepListed returns the values of listed that asked names, once each and in
// the order of listed.
func keepListed(asked, listed []string) []string {
	if len(asked) == 0 {
		return nil
	}
	var out []string
	for _, value := range listed {
		if slices.Contains(asked, value) {
			out = append(out, value)
		}
	}
	return out
}

// ListingState is what a request asked of a listing, after the listing's spec
// has had its say: every value in it is one the spec declared.
//
// It is a value. The methods that change it return the state that follows and
// leave the receiver alone, applying the rules a table keeps: a change of the
// search or of a filter goes back to the first page, a change of the page size
// keeps the first row that was on screen, and a hidden column changes nothing
// else. [ListingState.Values] writes it back as a canonical query string, which
// is what every link of the table is made from, so the address never gathers a
// parameter it does not need.
//
// The zero value belongs to no listing: it filters nothing and orders by
// nothing, and every method on it leaves it as it is.
type ListingState struct {
	listing *Listing
	search  string
	sort    string
	dir     SortDirection
	facets  map[string][]string
	page    int
	perPage int
	hidden  []string
}

// Listing returns the listing this state was read by, nil for the zero value.
func (s ListingState) Listing() *Listing { return s.listing }

// Search returns the search text, trimmed.
func (s ListingState) Search() string { return s.search }

// SortKey returns the public key of the sort, empty when the listing is ordered
// by its tie-break alone.
func (s ListingState) SortKey() string { return s.sort }

// Direction returns the direction of the sort.
func (s ListingState) Direction() SortDirection {
	if s.dir == "" {
		return SortAsc
	}
	return s.dir
}

// Facet returns the values selected for the facet under key, in the order the
// spec lists them.
func (s ListingState) Facet(key string) []string { return slices.Clone(s.facets[key]) }

// FacetSelected reports whether value is selected for the facet under key.
func (s ListingState) FacetSelected(key, value string) bool {
	return slices.Contains(s.facets[key], value)
}

// Page returns the page being read, counting from one.
func (s ListingState) Page() int {
	if s.page < 1 {
		return 1
	}
	return s.page
}

// PerPage returns the page size. It is zero for the zero state, which belongs
// to no listing.
func (s ListingState) PerPage() int { return s.perPage }

// Offset returns how many rows come before the page.
func (s ListingState) Offset() int { return (s.Page() - 1) * s.perPage }

// Hidden returns the keys of the hidden columns, in the order the spec lists
// them.
func (s ListingState) Hidden() []string { return slices.Clone(s.hidden) }

// IsHidden reports whether the column under key is hidden.
func (s ListingState) IsHidden(key string) bool { return slices.Contains(s.hidden, key) }

// Filtered reports whether the search text or any facet narrows the rows.
func (s ListingState) Filtered() bool { return s.search != "" || len(s.facets) > 0 }

// SearchColumns returns the columns the search text is looked for in.
func (s ListingState) SearchColumns() []string {
	if s.listing == nil {
		return nil
	}
	return s.listing.SearchColumns()
}

// FacetFilter is a facet that narrows the rows: its column and the values
// selected, all of them declared by the spec.
type FacetFilter struct {
	Key    string
	Column string
	Values []string
}

// FacetFilters returns the facets that have a value selected, in the order the
// spec declares them, leaving out the facet under except. Passing an empty
// except leaves nothing out.
func (s ListingState) FacetFilters(except string) []FacetFilter {
	if s.listing == nil {
		return nil
	}
	var out []FacetFilter
	for _, facet := range s.listing.facets {
		values := s.facets[facet.Key]
		if len(values) == 0 || facet.Key == except {
			continue
		}
		out = append(out, FacetFilter{Key: facet.Key, Column: facet.Column, Values: slices.Clone(values)})
	}
	return out
}

// SortOrder is one column of the order a state asks for.
type SortOrder struct {
	Column string
	Desc   bool
}

// Order returns the order the rows are read in: the sort when there is one, and
// then the tie-break, in the same direction so that reversing the sort reverses
// the whole list. The columns come from the spec, never from the request.
func (s ListingState) Order() []SortOrder {
	if s.listing == nil {
		return nil
	}
	desc := s.Direction() == SortDesc
	var out []SortOrder
	if column, ok := s.listing.sorts[s.sort]; ok && column != s.listing.tieBreak {
		out = append(out, SortOrder{Column: column, Desc: desc})
	}
	return append(out, SortOrder{Column: s.listing.tieBreak, Desc: desc})
}

// with returns a copy of s that can be changed without touching s.
func (s ListingState) with() ListingState {
	if s.facets != nil {
		facets := make(map[string][]string, len(s.facets))
		for key, values := range s.facets {
			facets[key] = values
		}
		s.facets = facets
	}
	return s
}

// WithSearch returns the state searching for text, back on the first page.
func (s ListingState) WithSearch(text string) ListingState {
	if s.listing == nil {
		return s
	}
	s = s.with()
	s.search = cleanSearch(text)
	s.page = 1
	return s
}

// WithSort returns the state ordered by the column under key, in dir, back on
// the first page. A key the spec did not declare changes nothing, and neither
// does a direction other than asc and desc.
func (s ListingState) WithSort(key string, dir SortDirection) ListingState {
	if s.listing == nil || !s.listing.Sortable(key) || (dir != SortAsc && dir != SortDesc) {
		return s
	}
	s.sort, s.dir, s.page = key, dir, 1
	return s
}

// ToggleSort returns the state a click on the header of the column under key
// leads to: the column ascending when it was not the sort, and the opposite
// direction when it was. It never goes back to no sort. A key the spec did not
// declare changes nothing.
func (s ListingState) ToggleSort(key string) ListingState {
	if s.listing == nil || !s.listing.Sortable(key) {
		return s
	}
	if s.sort == key {
		return s.WithSort(key, s.Direction().Flip())
	}
	return s.WithSort(key, SortAsc)
}

// WithFacet returns the state with exactly values selected for the facet under
// key, back on the first page. Values the facet does not list are dropped, and
// none left clears the facet. An unknown key changes nothing.
func (s ListingState) WithFacet(key string, values ...string) ListingState {
	facet, ok := s.facetSpec(key)
	if !ok {
		return s
	}
	s = s.with()
	if chosen := keepListed(values, facet.Values); len(chosen) > 0 {
		if s.facets == nil {
			s.facets = map[string][]string{}
		}
		s.facets[key] = chosen
	} else {
		delete(s.facets, key)
	}
	s.page = 1
	return s
}

// ToggleFacet returns the state with value selected for the facet under key
// when it was not, and unselected when it was, back on the first page. A key or
// value the spec did not declare changes nothing.
func (s ListingState) ToggleFacet(key, value string) ListingState {
	facet, ok := s.facetSpec(key)
	if !ok || !slices.Contains(facet.Values, value) {
		return s
	}
	current := slices.Clone(s.facets[key])
	if i := slices.Index(current, value); i >= 0 {
		current = slices.Delete(current, i, i+1)
	} else {
		current = append(current, value)
	}
	return s.WithFacet(key, current...)
}

// ClearFacet returns the state with nothing selected for the facet under key.
func (s ListingState) ClearFacet(key string) ListingState { return s.WithFacet(key) }

// ClearFilters returns the state without a search text and without a facet
// selected, back on the first page. The sort, the page size and the hidden
// columns stay.
func (s ListingState) ClearFilters() ListingState {
	if s.listing == nil {
		return s
	}
	s.search, s.facets, s.page = "", nil, 1
	return s
}

func (s ListingState) facetSpec(key string) (FacetSpec, bool) {
	if s.listing == nil {
		return FacetSpec{}, false
	}
	return s.listing.Facet(key)
}

// WithPage returns the state on page n. Below one is the first page.
func (s ListingState) WithPage(n int) ListingState {
	if s.listing == nil {
		return s
	}
	s.page = min(max(n, 1), maxListingPage)
	return s
}

// WithPageSize returns the state with n rows to a page, on the page that still
// shows the first row the reader had on screen: from page 2 at 25 rows to 10
// rows, row 26 is first, and it is on page 3. A size the spec did not offer
// changes nothing.
func (s ListingState) WithPageSize(n int) ListingState {
	if s.listing == nil || !slices.Contains(s.listing.pageSizes, n) {
		return s
	}
	first := s.Offset()
	s.perPage = n
	s.page = min(first/n+1, maxListingPage)
	return s
}

// Clamp returns the state on the last page when its page is past it, for a
// result of total rows. The state of a listing that matches nothing is on the
// first page.
func (s ListingState) Clamp(total int) ListingState {
	if s.listing == nil || s.perPage < 1 {
		return s
	}
	last := max((total+s.perPage-1)/s.perPage, 1)
	if s.page > last {
		s.page = last
	}
	return s
}

// WithHidden returns the state with the column under key hidden or shown. A key
// the spec did not declare as hideable changes nothing.
func (s ListingState) WithHidden(key string, hidden bool) ListingState {
	if s.listing == nil || !slices.Contains(s.listing.hideable, key) {
		return s
	}
	current := slices.Clone(s.hidden)
	if hidden {
		current = append(current, key)
	} else if i := slices.Index(current, key); i >= 0 {
		current = slices.Delete(current, i, i+1)
	}
	s.hidden = keepListed(current, s.listing.hideable)
	return s
}

// ToggleHidden returns the state with the column under key shown when it was
// hidden and hidden when it was shown.
func (s ListingState) ToggleHidden(key string) ListingState {
	return s.WithHidden(key, !s.IsHidden(key))
}

// Values writes the state as the query string that reads back to it.
//
// The form is canonical, so the same state always gives the same string: a
// parameter appears once, a repeated one in the order the spec lists its
// values, and a parameter whose value is the default is left out -- an empty
// search, the default sort in its default direction, page one, the default
// page size, no facet and no hidden column. The address of a listing nobody
// touched has no query at all.
func (s ListingState) Values() url.Values {
	out := url.Values{}
	l := s.listing
	if l == nil {
		return out
	}
	if s.search != "" {
		out.Set(l.names.Search, s.search)
	}
	if s.sort != l.defaultSort {
		out.Set(l.names.Sort, s.sort)
	}
	if s.Direction() != l.defaultDirectionOf(s.sort) {
		out.Set(l.names.Dir, string(s.Direction()))
	}
	for _, facet := range l.facets {
		if values := s.facets[facet.Key]; len(values) > 0 {
			out[facet.Param] = slices.Clone(values)
		}
	}
	if s.Page() > 1 {
		out.Set(l.names.Page, strconv.Itoa(s.Page()))
	}
	if s.perPage != l.defaultSize {
		out.Set(l.names.PerPage, strconv.Itoa(s.perPage))
	}
	if len(s.hidden) > 0 {
		out[l.names.Hide] = slices.Clone(s.hidden)
	}
	return out
}

// Encode is Values encoded, with the parameters in the order of their names.
func (s ListingState) Encode() string { return s.Values().Encode() }

// URL returns path followed by the encoded state, with no "?" when the state
// has nothing to write.
func (s ListingState) URL(path string) string {
	if encoded := s.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

// PageOptions returns the [Options] a page of this listing builds its links
// from: path, the state's parameters carried onto every link, and the page
// parameter this listing names.
func (s ListingState) PageOptions(path string) Options {
	opts := Options{Path: path}
	if s.listing == nil {
		return opts
	}
	opts.PageName = s.listing.names.Page
	opts.Query = s.Values()
	opts.Query.Del(opts.PageName)
	return opts
}

// FacetCount is how many rows carry one value of a facet.
type FacetCount struct {
	// Value is the facet value.
	Value string

	// Count is how many rows match, with every other filter applied and the
	// facet's own left out. It is zero for a value no row carries.
	Count int

	// Selected reports whether the state has the value selected.
	Selected bool
}

// ListingResult is what a listing hands the component that draws it: the state
// that was read, the page with its total, and the count of rows per facet
// value.
//
// The rows are not in it, for the reason a page holds none: they travel beside
// it as whatever type the query returned.
type ListingResult struct {
	// State is the state the rows were read for. Its page is the page read: a
	// request past the last page is brought back to the last one.
	State ListingState

	// Page is the arithmetic of the page, with the total of the filtered rows.
	Page *LengthAwarePage

	facets map[string][]FacetCount
}

// NewListingResult assembles the result of a listing. counts holds, by facet
// key, the count of every value of the facet.
func NewListingResult(state ListingState, page *LengthAwarePage, counts map[string][]FacetCount) *ListingResult {
	facets := make(map[string][]FacetCount, len(counts))
	for key, values := range counts {
		facets[key] = slices.Clone(values)
	}
	return &ListingResult{State: state, Page: page, facets: facets}
}

// Total returns how many rows match the filters, whatever the page.
func (r *ListingResult) Total() int { return r.Page.Total() }

// FacetCounts returns the count of every value of the facet under key, in the
// order the spec lists the values.
func (r *ListingResult) FacetCounts(key string) []FacetCount {
	return slices.Clone(r.facets[key])
}
