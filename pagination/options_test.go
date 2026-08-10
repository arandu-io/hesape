package pagination_test

import (
	"net/url"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) = %v", raw, err)
	}
	return u
}

func TestOptionsFromKeepsPathAndQuery(t *testing.T) {
	opts := pagination.OptionsFrom(mustParse(t, "/users?team=core&page=7#list"))
	p := pagination.Paginate([]int{1}, 100, 10, 2, opts)

	if got, want := p.URL(3), "/users?page=3&team=core"; got != want {
		t.Errorf("URL(3) = %q, want %q", got, want)
	}
}

func TestOptionsFromKeepsSchemeAndHost(t *testing.T) {
	opts := pagination.OptionsFrom(mustParse(t, "https://arandu.test/users?sort=name"))
	p := pagination.Paginate([]int{1}, 100, 10, 1, opts)

	if got, want := p.URL(2), "https://arandu.test/users?page=2&sort=name"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestOptionsFromNilURL(t *testing.T) {
	p := pagination.Paginate([]int{1}, 1, 10, 1, pagination.OptionsFrom(nil))
	if got, want := p.URL(1), "/?page=1"; got != want {
		t.Errorf("URL(1) = %q, want %q", got, want)
	}
}

func TestZeroOptionsPaginateRoot(t *testing.T) {
	p := pagination.Paginate([]int{1}, 1, 10, 1, pagination.Options{})
	if got, want := p.URL(2), "/?page=2"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestOptionsTrimTrailingSlash(t *testing.T) {
	p := pagination.Paginate([]int{1}, 1, 10, 1, pagination.Options{Path: "/users/"})
	if got, want := p.URL(2), "/users?page=2"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestOptionsPathWithQueryUsesAmpersand(t *testing.T) {
	p := pagination.Paginate([]int{1}, 1, 10, 1, pagination.Options{Path: "/index.php?route=users"})
	if got, want := p.URL(2), "/index.php?route=users&page=2"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestOptionsFragment(t *testing.T) {
	p := pagination.Paginate([]int{1}, 1, 10, 1, pagination.Options{Path: "/users", Fragment: "list"})
	if got, want := p.URL(2), "/users?page=2#list"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestOptionsCustomPageName(t *testing.T) {
	opts := pagination.Options{Path: "/users", PageName: "p"}
	p := pagination.Paginate([]int{1}, 100, 10, 1, opts)
	if got, want := p.URL(4), "/users?p=4"; got != want {
		t.Errorf("URL(4) = %q, want %q", got, want)
	}
}

// The page the reader is on must never survive into the link of another page,
// whatever it is called.
func TestOptionsDropIncomingPageAndCursor(t *testing.T) {
	opts := pagination.Options{
		Path:  "/users",
		Query: url.Values{"page": {"9"}, "cursor": {"abc"}, "team": {"core"}},
	}
	p := pagination.Paginate([]int{1}, 100, 10, 9, opts)
	if got, want := p.URL(2), "/users?page=2&team=core"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

// A paginator that changed under the map its caller kept would render one thing
// and link another.
func TestOptionsQueryIsCopied(t *testing.T) {
	query := url.Values{"team": {"core"}}
	p := pagination.Paginate([]int{1}, 100, 10, 1, pagination.Options{Path: "/users", Query: query})
	query.Set("team", "view")

	if got, want := p.URL(2), "/users?page=2&team=core"; got != want {
		t.Errorf("URL(2) = %q, want %q", got, want)
	}
}

func TestResolveResolveCurrentPage(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"/users?page=3", 3},
		{"/users", 1},
		{"/users?page=", 1},
		{"/users?page=0", 1},
		{"/users?page=-3", 1},
		{"/users?page=two", 1},
		{"/users?page=1e3", 1},
		{"/users?page=2.5", 1},
		{"/users?page=999999", 999999},
	}
	for _, c := range cases {
		if got := pagination.ResolveCurrentPage(mustParse(t, c.raw), ""); got != c.want {
			t.Errorf("ResolveResolveCurrentPage(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestResolveCurrentPageCustomName(t *testing.T) {
	u := mustParse(t, "/users?p=4&page=9")
	if got := pagination.ResolveCurrentPage(u, "p"); got != 4 {
		t.Errorf("ResolveCurrentPage = %d, want 4", got)
	}
}

func TestResolveCurrentPageNilURL(t *testing.T) {
	if got := pagination.ResolveCurrentPage(nil, ""); got != 1 {
		t.Errorf("ResolveResolveCurrentPage(nil) = %d, want 1", got)
	}
}
