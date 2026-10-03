package pagination_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// The probe row is the whole mechanism: it settles "is there a next page"
// without counting, and the reader must never see it.
func TestSimplePaginateDropsTheProbeRow(t *testing.T) {
	p := pagination.NewPage(11, 10, 1, pagination.Options{Path: "/users"})

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
	p := pagination.NewPage(10, 10, 3, pagination.Options{Path: "/users"})

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
	p := pagination.NewPage(4, 10, 1, pagination.Options{Path: "/users"})

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
	p := pagination.NewPage(11, 10, 4, pagination.Options{})
	if got, want := p.FirstItem(), 31; got != want {
		t.Errorf("FirstItem = %d, want %d", got, want)
	}
	if got, want := p.LastItem(), 40; got != want {
		t.Errorf("LastItem = %d, want %d", got, want)
	}

	empty := pagination.NewPage(0, 10, 4, pagination.Options{})
	if empty.FirstItem() != 0 || empty.LastItem() != 0 {
		t.Errorf("empty page item range = %d..%d, want 0..0", empty.FirstItem(), empty.LastItem())
	}
}

func TestSimplePaginateGuardsAgainstNonsenseInput(t *testing.T) {
	p := pagination.NewPage(3, 0, -2, pagination.Options{})
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

func TestNewPageReadsANegativeCountAsEmpty(t *testing.T) {
	p := pagination.NewPage(-3, 10, 1, pagination.Options{})
	if !p.IsEmpty() || p.IsNotEmpty() || p.Count() != 0 {
		t.Errorf("Count = %d, want an empty page", p.Count())
	}
	if p.HasMorePages() {
		t.Error("HasMorePages = true, want false")
	}
}

// The page holds no rows, so the rows reach the payload through the argument,
// and a page encoded on its own is the arithmetic and nothing else.
func TestPagePayloadCarriesTheRowsItIsGiven(t *testing.T) {
	p := pagination.NewPage(3, 2, 1, pagination.Options{Path: "/users"})

	payload := p.ToArray([]int{1, 2})
	if got, ok := payload["data"].([]int); !ok || len(got) != 2 {
		t.Errorf("data = %v, want the rows handed to ToArray", payload["data"])
	}
	if payload["next_page_url"] != "/users?page=2" || payload["current_page_url"] != "/users?page=1" {
		t.Errorf("payload = %v, want the next page and the current page linked", payload)
	}

	pretty, err := p.ToPrettyJSON([]int{1, 2})
	if err != nil {
		t.Fatalf("ToPrettyJSON: %v", err)
	}
	if !strings.Contains(string(pretty), "\n    \"data\"") {
		t.Errorf("ToPrettyJSON = %s, want the rows under data, indented four spaces", pretty)
	}

	alone, err := p.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(alone), `"data"`) {
		t.Errorf("MarshalJSON = %s, want no data key: the page holds no rows", alone)
	}
}
