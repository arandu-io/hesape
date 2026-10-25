package pagination_test

import (
	"math/rand"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// taskListing is a listing shaped like the Tasks table of the reference: a
// searchable title, three sortable columns, two facets, the six page sizes and
// three columns a reader may hide.
func taskListing(t *testing.T) *pagination.Listing {
	t.Helper()
	listing, err := pagination.NewListing(pagination.ListingSpec{
		Search: []string{"title", "code"},
		Sorts: map[string]string{
			"title":    "tasks.title",
			"status":   "tasks.status",
			"priority": "tasks.priority",
			"created":  "tasks.created_at",
		},
		DefaultSort: "created",
		DefaultDir:  pagination.SortDesc,
		TieBreak:    "tasks.id",
		Facets: []pagination.FacetSpec{
			{Key: "status", Column: "tasks.status", Values: []string{"todo", "in progress", "done", "canceled"}},
			{Key: "priority", Param: "prio", Column: "tasks.priority", Values: []string{"low", "medium", "high"}},
		},
		PageSizes:       []int{10, 20, 25, 30, 40, 50},
		DefaultPageSize: 25,
		Hideable:        []string{"title", "status", "priority"},
	})
	if err != nil {
		t.Fatalf("NewListing: %v", err)
	}
	return listing
}

func parse(t *testing.T, raw string) pagination.ListingState {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", raw, err)
	}
	return taskListing(t).Parse(values)
}

func TestAnEmptyQueryReadsTheDefaults(t *testing.T) {
	s := parse(t, "")
	if s.Search() != "" || s.SortKey() != "created" || s.Direction() != pagination.SortDesc ||
		s.Page() != 1 || s.PerPage() != 25 || len(s.Hidden()) != 0 || s.Filtered() {
		t.Fatalf("defaults wrong: search=%q sort=%q dir=%q page=%d per=%d hidden=%v filtered=%v",
			s.Search(), s.SortKey(), s.Direction(), s.Page(), s.PerPage(), s.Hidden(), s.Filtered())
	}
	if got := s.Encode(); got != "" {
		t.Fatalf("the defaults must encode to nothing, got %q", got)
	}
	if got := s.URL("/tasks"); got != "/tasks" {
		t.Fatalf("URL = %q, want the bare path", got)
	}
}

// TestASortOutsideTheWhitelistFallsBackToTheDefault: the key is looked up, never
// used, so a column name that was not declared -- or an injection -- is the
// default sort and nothing else.
func TestASortOutsideTheWhitelistFallsBackToTheDefault(t *testing.T) {
	for _, key := range []string{"password", "tasks.title", "title; drop table tasks", "TITLE", " title", "id", ""} {
		s := parse(t, url.Values{"sort": {key}}.Encode())
		if s.SortKey() != "created" {
			t.Errorf("sort=%q read as %q, want the default sort", key, s.SortKey())
		}
		for _, order := range s.Order() {
			if order.Column != "tasks.created_at" && order.Column != "tasks.id" {
				t.Errorf("sort=%q reached the order as column %q", key, order.Column)
			}
		}
	}
	if s := parse(t, "sort=title"); s.SortKey() != "title" {
		t.Fatalf("a declared sort must be kept, got %q", s.SortKey())
	}
}

func TestTheDirectionIsAscOrDescAndOtherwiseTheSortsDefault(t *testing.T) {
	cases := []struct {
		query string
		want  pagination.SortDirection
	}{
		{"", pagination.SortDesc},                     // the default sort keeps its own direction
		{"dir=asc", pagination.SortAsc},               // asked for
		{"dir=desc", pagination.SortDesc},             //
		{"dir=sideways", pagination.SortDesc},         // invalid: the default sort's direction
		{"sort=title", pagination.SortAsc},            // any other sort is ascending
		{"sort=title&dir=DESC", pagination.SortAsc},   // the case is not folded: invalid
		{"sort=title&dir=desc", pagination.SortDesc},  //
		{"sort=title&dir=1%3B--", pagination.SortAsc}, //
		{"sort=created&dir=bad", pagination.SortDesc}, //
	}
	for _, c := range cases {
		if got := parse(t, c.query).Direction(); got != c.want {
			t.Errorf("%q: direction = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestFacetsKeepOnlyListedValuesOnceInTheOrderOfTheSpec(t *testing.T) {
	s := parse(t, "status=done&status=bogus&status=todo&status=done&status=&prio=high&prio=HIGH")
	if got, want := s.Facet("status"), []string{"todo", "done"}; !reflect.DeepEqual(got, want) {
		t.Errorf("status = %v, want %v (listed, deduplicated, in spec order)", got, want)
	}
	if got, want := s.Facet("priority"), []string{"high"}; !reflect.DeepEqual(got, want) {
		t.Errorf("priority = %v, want %v: the facet travels in the parameter prio", got, want)
	}
	if !s.FacetSelected("status", "done") || s.FacetSelected("status", "canceled") {
		t.Error("FacetSelected disagrees with the values")
	}
	if got := s.Facet("nothing"); len(got) != 0 {
		t.Errorf("an undeclared facet read as %v", got)
	}
	if !s.Filtered() {
		t.Error("a state with a facet is filtered")
	}
}

// TestAFacetParameterThatIsNotTheKeyIsTheOnlyOneRead: the key names the facet in
// code and the parameter names it on the wire, and only the parameter is read.
func TestAFacetParameterThatIsNotTheKeyIsTheOnlyOneRead(t *testing.T) {
	if got := parse(t, "priority=high").Facet("priority"); len(got) != 0 {
		t.Fatalf("the key was read as a parameter: %v", got)
	}
}

func TestThePageIsAWholeNumberFromOne(t *testing.T) {
	cases := map[string]int{
		"": 1, "0": 1, "-4": 1, "two": 1, "1e3": 1, "2.5": 1,
		"3": 3, " 4 ": 4, "99999999999999999999": 1, "5000000": 1_000_000,
	}
	for value, want := range cases {
		state := taskListing(t).Parse(url.Values{"page": {value}})
		if got := state.Page(); got != want {
			t.Errorf("page=%q: page = %d, want %d", value, got, want)
		}
	}
}

func TestThePageSizeIsOneOfTheOffered(t *testing.T) {
	cases := map[string]int{"per_page=10": 10, "per_page=50": 50, "per_page=15": 25, "per_page=0": 25,
		"per_page=-10": 25, "per_page=x": 25, "per_page=": 25, "": 25}
	for raw, want := range cases {
		if got := parse(t, raw).PerPage(); got != want {
			t.Errorf("%q: per page = %d, want %d", raw, got, want)
		}
	}
}

func TestOnlyHideableColumnsAreHiddenOnceEachInTheOrderOfTheSpec(t *testing.T) {
	s := parse(t, "hide=priority&hide=password&hide=title&hide=priority")
	if got, want := s.Hidden(), []string{"title", "priority"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden = %v, want %v", got, want)
	}
	if !s.IsHidden("title") || s.IsHidden("status") || s.IsHidden("password") {
		t.Fatal("IsHidden disagrees with Hidden")
	}
}

func TestTheSearchTextIsCleanedForTheDatabase(t *testing.T) {
	long := strings.Repeat("ab", 300)
	cases := []struct{ in, want string }{
		{"  hello  ", "hello"},
		{"a\x00b", "ab"}, // Postgres refuses a NUL in a text parameter
		{"a\xffb", "ab"}, // and a byte that is not UTF-8
		{"tab\tand\nnewline", "tabandnewline"},
		{"ção", "ção"},
		{long, long[:200]},
	}
	for _, c := range cases {
		s := taskListing(t).Parse(url.Values{"q": {c.in}})
		if s.Search() != c.want {
			t.Errorf("q=%q read as %q, want %q", c.in, s.Search(), c.want)
		}
	}
}

func TestTheOrderIsTheSortThenTheTieBreakInTheSameDirection(t *testing.T) {
	cases := []struct {
		query string
		want  []pagination.SortOrder
	}{
		{"", []pagination.SortOrder{{Column: "tasks.created_at", Desc: true}, {Column: "tasks.id", Desc: true}}},
		{"sort=title", []pagination.SortOrder{{Column: "tasks.title"}, {Column: "tasks.id"}}},
		{"sort=title&dir=desc", []pagination.SortOrder{{Column: "tasks.title", Desc: true}, {Column: "tasks.id", Desc: true}}},
	}
	for _, c := range cases {
		if got := parse(t, c.query).Order(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: order = %v, want %v", c.query, got, c.want)
		}
	}

	// A listing with no default sort is ordered by the tie-break alone, and a
	// sort that is the tie-break is not written twice.
	plain, err := pagination.NewListing(pagination.ListingSpec{Sorts: map[string]string{"id": "id", "name": "name"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plain.Parse(nil).Order(), []pagination.SortOrder{{Column: "id"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("no sort: order = %v, want %v", got, want)
	}
	if got, want := plain.Parse(url.Values{"sort": {"id"}}).Order(), []pagination.SortOrder{{Column: "id"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sort on the tie-break: order = %v, want %v", got, want)
	}
}

func TestAChangeOfTheSearchOrOfAFacetGoesBackToThePageOne(t *testing.T) {
	deep := parse(t, "page=4&q=old&status=todo")
	if deep.Page() != 4 {
		t.Fatalf("page = %d", deep.Page())
	}
	changes := map[string]pagination.ListingState{
		"WithSearch":   deep.WithSearch("new"),
		"WithFacet":    deep.WithFacet("status", "done"),
		"ToggleFacet":  deep.ToggleFacet("priority", "high"),
		"ClearFacet":   deep.ClearFacet("status"),
		"ClearFilters": deep.ClearFilters(),
		"ToggleSort":   deep.ToggleSort("title"),
		"WithSort":     deep.WithSort("title", pagination.SortDesc),
	}
	for name, next := range changes {
		if next.Page() != 1 {
			t.Errorf("%s left the state on page %d", name, next.Page())
		}
	}
	if deep.Page() != 4 || deep.Search() != "old" {
		t.Fatal("a method changed the state it was called on")
	}

	// Hiding a column and going to a page are not changes to the rows.
	if got := deep.ToggleHidden("title").Page(); got != 4 {
		t.Errorf("hiding a column moved the page to %d", got)
	}
}

func TestChangingThePageSizeKeepsTheFirstVisibleRow(t *testing.T) {
	// The reference: from page 2 at 25 rows, going to 10 rows lands on page 3,
	// because row 26 is the first one on screen and it is the sixth row of page 3.
	s := parse(t, "page=2&per_page=25").WithPageSize(10)
	if s.PerPage() != 10 || s.Page() != 3 {
		t.Fatalf("page 2 at 25 -> 10 gave page %d at %d, want page 3 at 10", s.Page(), s.PerPage())
	}
	// And back: row 21 is first on page 3 at 10 rows, and it is on page 1 at 25.
	if s = s.WithPageSize(25); s.Page() != 1 {
		t.Fatalf("page 3 at 10 -> 25 gave page %d, want 1", s.Page())
	}
	// The first row never leaves the page.
	for page := 1; page <= 12; page++ {
		for _, from := range []int{10, 20, 25, 30, 40, 50} {
			for _, to := range []int{10, 20, 25, 30, 40, 50} {
				start := parse(t, "").WithPageSize(from).WithPage(page)
				first := start.Offset()
				next := start.WithPageSize(to)
				if next.Offset() > first || first >= next.Offset()+to {
					t.Fatalf("page %d at %d -> %d: row offset %d is not on the page that begins at %d",
						page, from, to, first, next.Offset())
				}
			}
		}
	}
	// A size that is not offered changes nothing.
	if got := parse(t, "page=3").WithPageSize(15); got.PerPage() != 25 || got.Page() != 3 {
		t.Fatalf("an unoffered size changed the state: page %d at %d", got.Page(), got.PerPage())
	}
}

func TestTheMethodsRefuseWhatTheSpecDidNotDeclare(t *testing.T) {
	s := parse(t, "page=2&sort=title&status=todo")
	same := map[string]pagination.ListingState{
		"unknown sort":     s.ToggleSort("password"),
		"unknown sort dir": s.WithSort("title", "sideways"),
		"unknown facet":    s.ToggleFacet("owner", "ana"),
		"unlisted value":   s.ToggleFacet("status", "bogus"),
		"unknown hidden":   s.ToggleHidden("password"),
	}
	for name, next := range same {
		if !reflect.DeepEqual(next.Values(), s.Values()) {
			t.Errorf("%s changed the state: %v -> %v", name, s.Values(), next.Values())
		}
	}
	if got := s.WithFacet("status", "bogus").Facet("status"); len(got) != 0 {
		t.Errorf("WithFacet kept an unlisted value: %v", got)
	}
}

func TestTheSortCycleNeverGoesBackToNoSort(t *testing.T) {
	s := parse(t, "")
	s = s.ToggleSort("title")
	if s.SortKey() != "title" || s.Direction() != pagination.SortAsc {
		t.Fatalf("first click: %s %s, want title asc", s.SortKey(), s.Direction())
	}
	s = s.ToggleSort("title")
	if s.Direction() != pagination.SortDesc {
		t.Fatalf("second click: %s, want desc", s.Direction())
	}
	s = s.ToggleSort("title")
	if s.Direction() != pagination.SortAsc || s.SortKey() != "title" {
		t.Fatalf("third click: %s %s, want asc", s.SortKey(), s.Direction())
	}
}

func TestToggleFacetAddsAndRemovesAndKeepsTheSpecOrder(t *testing.T) {
	s := parse(t, "")
	s = s.ToggleFacet("status", "done").ToggleFacet("status", "todo")
	if got, want := s.Facet("status"), []string{"todo", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("status = %v, want %v", got, want)
	}
	s = s.ToggleFacet("status", "done")
	if got, want := s.Facet("status"), []string{"todo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("status = %v, want %v", got, want)
	}
	if s = s.ToggleFacet("status", "todo"); s.Filtered() || s.Encode() != "" {
		t.Fatalf("the last value off must clear the facet, got %q", s.Encode())
	}
}

func TestClearFiltersKeepsTheViewAndDropsTheFilters(t *testing.T) {
	s := parse(t, "q=x&status=todo&prio=low&sort=title&dir=desc&per_page=40&hide=title&page=3").ClearFilters()
	want := "dir=desc&hide=title&per_page=40&sort=title"
	if got := s.Encode(); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
}

// TestValuesAreCanonicalAndOmitTheDefaults: the table writes every link from
// this, so the address must be one string for one state.
func TestValuesAreCanonicalAndOmitTheDefaults(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"page=1&per_page=25&sort=created&dir=desc&q=", ""},
		{"status=done&status=todo&status=done", "status=todo&status=done"},
		{"q=hello&page=3&sort=title", "page=3&q=hello&sort=title"},
		{"sort=created&dir=asc", "dir=asc"},
		{"sort=title&dir=asc", "sort=title"},
		{"sort=title&dir=desc", "dir=desc&sort=title"},
		{"hide=priority&hide=title", "hide=title&hide=priority"},
		{"per_page=10&prio=high&prio=low", "per_page=10&prio=low&prio=high"},
		{"unknown=1&sort=nope&page=0", ""},
	}
	for _, c := range cases {
		if got := parse(t, c.in).Encode(); got != c.want {
			t.Errorf("%q encodes to %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTheEncodedStateReadsBackToItself: the property that keeps the address
// from drifting. It holds over states made by the methods and by hand-edited
// queries alike.
func TestTheEncodedStateReadsBackToItself(t *testing.T) {
	listing := taskListing(t)
	r := rand.New(rand.NewSource(7))
	keys := []string{"sort", "dir", "q", "page", "per_page", "hide", "status", "prio", "junk"}
	words := []string{"title", "created", "desc", "asc", "todo", "done", "high", "x y", "3", "0", "-1", "10", "25", "bogus", ""}
	for range 2000 {
		values := url.Values{}
		for range r.Intn(8) {
			key := keys[r.Intn(len(keys))]
			values.Add(key, words[r.Intn(len(words))])
		}
		first := listing.Parse(values)
		second := listing.Parse(first.Values())
		if !reflect.DeepEqual(first.Values(), second.Values()) || first.Page() != second.Page() ||
			first.PerPage() != second.PerPage() || first.SortKey() != second.SortKey() ||
			first.Direction() != second.Direction() || first.Search() != second.Search() {
			t.Fatalf("%v -> %q does not read back to itself: %q", values, first.Encode(), second.Encode())
		}
		// The method that rebuilds the state from its own parts agrees.
		if got, want := first.WithPage(first.Page()).Encode(), first.Encode(); got != want {
			t.Fatalf("WithPage(Page()) changed %q to %q", want, got)
		}
	}
}

func TestPageOptionsCarryTheStateOntoEveryLinkExceptThePage(t *testing.T) {
	s := parse(t, "q=hello&status=todo&page=2&sort=title")
	opts := s.PageOptions("/tasks")
	page := pagination.NewLengthAwarePage(25, 100, s.PerPage(), s.Page(), opts)
	if got, want := page.URL(3), "/tasks?page=3&q=hello&sort=title&status=todo"; got != want {
		t.Fatalf("URL(3) = %q, want %q", got, want)
	}
	if opts.PageName != "page" {
		t.Fatalf("PageName = %q", opts.PageName)
	}
}

func TestClampBringsAPastTheEndPageBack(t *testing.T) {
	s := parse(t, "page=9&per_page=10")
	if got := s.Clamp(25).Page(); got != 3 {
		t.Errorf("25 rows at 10: page = %d, want 3", got)
	}
	if got := s.Clamp(0).Page(); got != 1 {
		t.Errorf("no rows: page = %d, want 1", got)
	}
	if got := s.Clamp(1000).Page(); got != 9 {
		t.Errorf("1000 rows: page = %d, want 9", got)
	}
}

func TestTheNamesOfTheParametersAreTheOnesOverridden(t *testing.T) {
	listing, err := pagination.NewListing(pagination.ListingSpec{
		Search:   []string{"name"},
		Sorts:    map[string]string{"name": "name"},
		Facets:   []pagination.FacetSpec{{Key: "kind", Column: "kind", Values: []string{"a", "b"}}},
		Hideable: []string{"name"},
		Names:    pagination.ListingNames{Search: "search", Sort: "order", Dir: "way", Page: "p", PerPage: "size", Hide: "cols"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := listing.Parse(url.Values{"search": {"x"}, "order": {"name"}, "way": {"desc"}, "p": {"2"}, "size": {"20"}, "cols": {"name"}, "kind": {"b"},
		"q": {"ignored"}, "sort": {"ignored"}, "page": {"9"}})
	if s.SortKey() != "name" || s.Page() != 2 || s.Search() != "x" || s.PerPage() != 20 {
		t.Fatalf("the overridden names were not read: %+v", s.Values())
	}
	want := "cols=name&kind=b&order=name&p=2&search=x&size=20&way=desc"
	if got := s.Encode(); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
}

func TestNewListingRefusesASpecThatContradictsItself(t *testing.T) {
	cases := map[string]pagination.ListingSpec{
		"a default sort that is not a sort": {Sorts: map[string]string{"a": "a"}, DefaultSort: "b"},
		"an empty sort column":              {Sorts: map[string]string{"a": ""}},
		"an empty search column":            {Search: []string{" "}},
		"a bad default direction":           {DefaultDir: "up"},
		"a facet without values":            {Facets: []pagination.FacetSpec{{Key: "k", Column: "c"}}},
		"a facet without a column":          {Facets: []pagination.FacetSpec{{Key: "k", Values: []string{"a"}}}},
		"a facet declared twice": {Facets: []pagination.FacetSpec{
			{Key: "k", Column: "c", Values: []string{"a"}}, {Key: "k", Column: "d", Values: []string{"a"}}}},
		"a facet on a reserved parameter": {Facets: []pagination.FacetSpec{{Key: "q", Column: "c", Values: []string{"a"}}}},
		"two facets on one parameter": {Facets: []pagination.FacetSpec{
			{Key: "a", Param: "x", Column: "c", Values: []string{"a"}}, {Key: "b", Param: "x", Column: "d", Values: []string{"a"}}}},
		"a default page size that is not offered": {PageSizes: []int{10, 20}, DefaultPageSize: 15},
		"a page size below one":                   {PageSizes: []int{0, 10}},
		"two names that collide":                  {Names: pagination.ListingNames{Sort: "q"}},
		"an empty hideable key":                   {Hideable: []string{""}},
	}
	for name, spec := range cases {
		if listing, err := pagination.NewListing(spec); err == nil || listing != nil {
			t.Errorf("%s: NewListing accepted the spec", name)
		}
	}
	if _, err := pagination.NewListing(pagination.ListingSpec{}); err != nil {
		t.Fatalf("the empty spec is a listing ordered by id: %v", err)
	}
}

func TestTheDefaultsOfASpec(t *testing.T) {
	listing, err := pagination.NewListing(pagination.ListingSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listing.PageSizes(), pagination.DefaultPageSizes) || listing.DefaultPageSize() != 10 {
		t.Fatalf("sizes %v default %d", listing.PageSizes(), listing.DefaultPageSize())
	}
	names := listing.Names()
	if names != (pagination.ListingNames{Search: "q", Sort: "sort", Dir: "dir", Page: "page", PerPage: "per_page", Hide: "hide"}) {
		t.Fatalf("names = %+v", names)
	}
	if got := listing.Parse(nil).Order(); !reflect.DeepEqual(got, []pagination.SortOrder{{Column: "id"}}) {
		t.Fatalf("order = %v", got)
	}

	small, err := pagination.NewListing(pagination.ListingSpec{PageSizes: []int{5, 15}})
	if err != nil {
		t.Fatal(err)
	}
	if small.DefaultPageSize() != 5 {
		t.Fatalf("with no 10 on offer the default is the first size, got %d", small.DefaultPageSize())
	}
}

// TestTheZeroStateBelongsToNoListing: it filters nothing and every method
// leaves it as it is, so a zero value reaching a query cannot name a column.
func TestTheZeroStateBelongsToNoListing(t *testing.T) {
	var s pagination.ListingState
	if s.Listing() != nil || s.Order() != nil || s.SearchColumns() != nil || s.FacetFilters("") != nil {
		t.Fatal("the zero state names columns")
	}
	changed := s.WithSearch("x").ToggleSort("a").WithFacet("a", "b").WithPageSize(10).ToggleHidden("a").WithPage(4).ClearFilters()
	if changed.Search() != "" || changed.Page() != 1 || changed.Encode() != "" {
		t.Fatalf("a method changed the zero state: %q", changed.Encode())
	}
	if got := s.PageOptions("/x"); got.Path != "/x" || got.PageName != "" {
		t.Fatalf("PageOptions = %+v", got)
	}
}

func TestAStateIsAValueTheMethodsDoNotAlias(t *testing.T) {
	base := parse(t, "status=todo&hide=title")
	a := base.ToggleFacet("status", "done")
	b := base.ToggleFacet("status", "canceled")
	if !reflect.DeepEqual(a.Facet("status"), []string{"todo", "done"}) || !reflect.DeepEqual(b.Facet("status"), []string{"todo", "canceled"}) {
		t.Fatalf("two branches of one state interfered: %v / %v", a.Facet("status"), b.Facet("status"))
	}
	if !reflect.DeepEqual(base.Facet("status"), []string{"todo"}) {
		t.Fatalf("the base changed: %v", base.Facet("status"))
	}
	got := base.Facet("status")
	got[0] = "tampered"
	if base.Facet("status")[0] != "todo" {
		t.Fatal("Facet returns the state's own slice")
	}
}

func TestFacetFiltersLeaveOutTheFacetBeingCounted(t *testing.T) {
	s := parse(t, "status=todo&prio=low&prio=high")
	all := s.FacetFilters("")
	if len(all) != 2 || all[0].Key != "status" || all[1].Key != "priority" || all[1].Column != "tasks.priority" {
		t.Fatalf("filters = %+v", all)
	}
	except := s.FacetFilters("status")
	if len(except) != 1 || except[0].Key != "priority" {
		t.Fatalf("filters without status = %+v", except)
	}
}

func TestParseURLReadsTheQueryOfARequest(t *testing.T) {
	listing := taskListing(t)
	u, _ := url.Parse("https://example.test/tasks?q=hi&page=2")
	if s := listing.ParseURL(u); s.Search() != "hi" || s.Page() != 2 {
		t.Fatalf("ParseURL = %q page %d", s.Search(), s.Page())
	}
	if s := listing.ParseURL(nil); s.Page() != 1 || s.Filtered() {
		t.Fatal("a nil URL must read as an empty query")
	}
}
