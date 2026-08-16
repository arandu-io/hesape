package pagination_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// The probe row is the whole mechanism: it settles "is there a next page"
// without counting, and the reader must never see it.
func TestSimplePaginateDropsTheProbeRow(t *testing.T) {
	p := pagination.SimplePaginate(rows(11), 10, 1, pagination.Options{Path: "/users"})

	if got := p.Count(); got != 10 {
		t.Errorf("Count = %d, want 10", got)
	}
	if !p.HasMorePages() {
		t.Error("HasMorePages = false, want true")
	}
	if got, want := p.NextPageURL(), "/users?page=2"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}
}

func TestSimplePaginateWithoutProbeRowIsTheLastPage(t *testing.T) {
	p := pagination.SimplePaginate(rows(10), 10, 3, pagination.Options{Path: "/users"})

	if p.HasMorePages() {
		t.Error("HasMorePages = true, want false")
	}
	if got := p.NextPageURL(); got != "" {
		t.Errorf("NextPageURL = %q, want empty", got)
	}
	if got, want := p.PreviousPageURL(), "/users?page=2"; got != want {
		t.Errorf("PreviousPageURL = %q, want %q", got, want)
	}
	if !p.HasPages() {
		t.Error("HasPages = false, want true: there is a page to go back to")
	}
}

func TestSimplePaginateFirstPageAlone(t *testing.T) {
	p := pagination.SimplePaginate(rows(4), 10, 1, pagination.Options{Path: "/users"})

	if !p.OnFirstPage() {
		t.Error("OnFirstPage = false, want true")
	}
	if p.HasPages() {
		t.Error("HasPages = true, want false: there is nowhere to go")
	}
	if got := p.PreviousPageURL(); got != "" {
		t.Errorf("PreviousPageURL = %q, want empty", got)
	}
}

func TestSimplePaginateItemRange(t *testing.T) {
	p := pagination.SimplePaginate(rows(11), 10, 4, pagination.Options{})
	if got, want := p.FirstItem(), 31; got != want {
		t.Errorf("FirstItem = %d, want %d", got, want)
	}
	if got, want := p.LastItem(), 40; got != want {
		t.Errorf("LastItem = %d, want %d", got, want)
	}

	empty := pagination.SimplePaginate(rows(0), 10, 4, pagination.Options{})
	if empty.FirstItem() != 0 || empty.LastItem() != 0 {
		t.Errorf("empty page item range = %d..%d, want 0..0", empty.FirstItem(), empty.LastItem())
	}
}

func TestSimplePaginateGuardsAgainstNonsenseInput(t *testing.T) {
	p := pagination.SimplePaginate(rows(3), 0, -2, pagination.Options{})
	if got := p.PerPage(); got != 1 {
		t.Errorf("PerPage = %d, want 1", got)
	}
	if got := p.CurrentPage(); got != 1 {
		t.Errorf("CurrentPage = %d, want 1", got)
	}
	if got := p.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}
}

func TestThroughSimple(t *testing.T) {
	p := pagination.SimplePaginate([]int{1, 2, 3, 4}, 3, 2, pagination.Options{Path: "/users"})
	mapped := pagination.ThroughSimple(p, strconv.Itoa)

	if got := mapped.Items(); !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Errorf("Items = %v, want the first three as strings", got)
	}
	if !mapped.HasMorePages() {
		t.Error("HasMorePages = false, want true")
	}
	if got, want := mapped.NextPageURL(), "/users?page=3"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}
}
