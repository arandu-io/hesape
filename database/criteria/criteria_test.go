package criteria_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/criteria"
)

// listing is the declaration every test reads against: four filters, two sorts,
// two fields and one include, with a page ceiling.
func listing() *criteria.Declaration {
	return criteria.Declare().
		Filter("status", "status", criteria.Exact).
		Filter("q", "name", criteria.Partial).
		Filter("created_after", "created_at", criteria.Above).
		Filter("kind", "kind", criteria.OneOf).
		Sort("name", "name").
		Sort("created", "created_at").
		Field("id", "id").
		Field("name", "name").
		Include("author", "Author").
		Page(15, 50)
}

func parse(t *testing.T, raw string) criteria.Plan {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q) = %v", raw, err)
	}
	plan, err := listing().Parse(values)
	if err != nil {
		t.Fatalf("Parse(%q) = %v", raw, err)
	}
	return plan
}

func refuse(t *testing.T, raw string) *criteria.Error {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q) = %v", raw, err)
	}
	_, err = listing().Parse(values)
	if err == nil {
		t.Fatalf("Parse(%q) was accepted", raw)
	}
	var refusal *criteria.Error
	if !errors.As(err, &refusal) {
		t.Fatalf("Parse(%q) = %v, want a *criteria.Error", raw, err)
	}
	return refusal
}

func TestAFilterNamesADeclarationAndTheDeclarationNamesTheColumn(t *testing.T) {
	plan := parse(t, "filter[q]=ana")

	if len(plan.Filters) != 1 {
		t.Fatalf("got %d filters, want 1: %+v", len(plan.Filters), plan.Filters)
	}
	got := plan.Filters[0]
	if got.Name != "q" || got.Column != "name" || got.Match != criteria.Partial || got.Value != "ana" {
		t.Errorf("filter = %+v, want the declared column and match", got)
	}
}

func TestAnUndeclaredFilterDoesNotExist(t *testing.T) {
	for _, raw := range []string{
		"filter[password]=x",
		"filter[users.password]=x",
		// The column name of a declared filter is not a filter: the public name
		// is the only name, which is what keeps the schema out of the URL.
		"filter[created_at]=2026-01-01",
	} {
		t.Run(raw, func(t *testing.T) {
			if got := refuse(t, raw).Reason; !strings.Contains(got, "no filter is declared") {
				t.Errorf("reason = %q", got)
			}
		})
	}
}

func TestAParameterAddressedToSomethingElseIsIgnored(t *testing.T) {
	plan := parse(t, "utm_source=mail&page=2&sort=name")

	if len(plan.Filters) != 0 {
		t.Errorf("got filters from a foreign parameter: %+v", plan.Filters)
	}
	if plan.Page != 2 {
		t.Errorf("Page = %d, want 2", plan.Page)
	}
}

func TestASingleValueFilterRefusesTwoValues(t *testing.T) {
	if got := refuse(t, "filter[status]=draft&filter[status]=sent").Reason; !strings.Contains(got, "more than once") {
		t.Errorf("reason = %q", got)
	}
}

func TestOneOfReadsAList(t *testing.T) {
	plan := parse(t, "filter[kind]=invoice,receipt&filter[kind]=note")

	if len(plan.Filters) != 1 {
		t.Fatalf("got %d filters, want 1", len(plan.Filters))
	}
	list, ok := plan.Filters[0].Value.([]string)
	if !ok {
		t.Fatalf("value = %T, want []string", plan.Filters[0].Value)
	}
	if len(list) != 3 {
		t.Errorf("list = %v, want three members", list)
	}
}

func TestAnEmptyValueIsNotAFilter(t *testing.T) {
	// A form that submits every control sends the ones nobody touched as empty,
	// and "name = ''" is not what the person asked for.
	plan := parse(t, "filter[q]=&filter[kind]=")
	if len(plan.Filters) != 0 {
		t.Errorf("an empty control became a clause: %+v", plan.Filters)
	}
}

func TestSort(t *testing.T) {
	plan := parse(t, "sort=-created,name")

	if len(plan.Orders) != 2 {
		t.Fatalf("got %d orders, want 2: %+v", len(plan.Orders), plan.Orders)
	}
	if plan.Orders[0].Column != "created_at" || !plan.Orders[0].Descending {
		t.Errorf("order 0 = %+v, want created_at descending", plan.Orders[0])
	}
	if plan.Orders[1].Column != "name" || plan.Orders[1].Descending {
		t.Errorf("order 1 = %+v, want name ascending", plan.Orders[1])
	}
}

func TestAnUndeclaredSortDoesNotExist(t *testing.T) {
	if got := refuse(t, "sort=secret").Reason; !strings.Contains(got, "no sort is declared") {
		t.Errorf("reason = %q", got)
	}
}

func TestFieldsAndIncludes(t *testing.T) {
	plan := parse(t, "fields=id,name&include=author")

	if len(plan.Columns) != 2 || plan.Columns[0] != "id" || plan.Columns[1] != "name" {
		t.Errorf("Columns = %v", plan.Columns)
	}
	if len(plan.Includes) != 1 || plan.Includes[0] != "Author" {
		t.Errorf("Includes = %v, want the declared relation", plan.Includes)
	}
}

func TestAnUndeclaredFieldOrIncludeDoesNotExist(t *testing.T) {
	if got := refuse(t, "fields=password").Reason; !strings.Contains(got, "no field is declared") {
		t.Errorf("reason = %q", got)
	}
	if got := refuse(t, "include=Owner").Reason; !strings.Contains(got, "no include is declared") {
		t.Errorf("reason = %q", got)
	}
}

func TestPaging(t *testing.T) {
	plan := parse(t, "")
	if plan.Page != 1 || plan.PerPage != 15 {
		t.Errorf("plan = page %d of %d, want the declared default", plan.Page, plan.PerPage)
	}

	plan = parse(t, "page=3&per_page=50")
	if plan.Page != 3 || plan.PerPage != 50 {
		t.Errorf("plan = page %d of %d, want 3 of 50", plan.Page, plan.PerPage)
	}
}

func TestPagingHasACeiling(t *testing.T) {
	if got := refuse(t, "per_page=1000000").Reason; !strings.Contains(got, "above the 50") {
		t.Errorf("reason = %q", got)
	}
}

func TestAPageThatIsNotANumberIsRefusedRatherThanReadAsZero(t *testing.T) {
	for _, raw := range []string{"page=0", "page=-1", "page=two", "per_page=0"} {
		t.Run(raw, func(t *testing.T) {
			refuse(t, raw)
		})
	}
}

func TestTheSameQueryStringTranslatesTheSameWay(t *testing.T) {
	// Filters come out of a map. Without an ordering, the same URL would
	// produce a different statement on every run, which a query cache and a
	// test both read as two different queries.
	raw := "filter[q]=ana&filter[status]=draft&filter[created_after]=2026-01-01"
	first := parse(t, raw)
	for range 20 {
		got := parse(t, raw)
		for i := range got.Filters {
			if got.Filters[i].Column != first.Filters[i].Column {
				t.Fatalf("clause %d = %q, first run had %q", i, got.Filters[i].Column, first.Filters[i].Column)
			}
		}
	}
}

func TestADeclarationRefusesWhatCouldNotHaveBeenMeant(t *testing.T) {
	for _, c := range []struct {
		name    string
		declare func()
	}{
		{"a column that is not an identifier", func() {
			criteria.Declare().Filter("q", "name; drop table users", criteria.Exact)
		}},
		{"a column with a space", func() {
			criteria.Declare().Filter("q", "name collate c", criteria.Exact)
		}},
		{"an empty column", func() { criteria.Declare().Sort("name", "") }},
		{"an empty name", func() { criteria.Declare().Filter("", "name", criteria.Exact) }},
		{"the same filter twice", func() {
			criteria.Declare().Filter("q", "name", criteria.Exact).Filter("q", "email", criteria.Exact)
		}},
		{"the same include twice", func() {
			criteria.Declare().Include("author", "Author").Include("author", "Owner")
		}},
		{"no comparison", func() { criteria.Declare().Filter("q", "name", 0) }},
		{"a ceiling below the default", func() { criteria.Declare().Page(50, 10) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("the declaration was accepted")
				}
			}()
			c.declare()
		})
	}
}

// A qualified column is the server's own writing and stays allowed: a join
// needs one, and it never came from a request.
func TestADeclarationAcceptsAQualifiedColumn(t *testing.T) {
	criteria.Declare().Filter("status", "invoices.status", criteria.Exact)
}
