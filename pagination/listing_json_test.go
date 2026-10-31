package pagination_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/pagination"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%T): %v", v, err)
	}
	return string(encoded)
}

func TestTheStateEncodesAsTheFullStateOfTheTable(t *testing.T) {
	cases := []struct {
		name  string
		state pagination.ListingState
		want  string
	}{
		{
			name:  "the defaults are written, not left out: a client has no spec to ask what they were",
			state: parse(t, ""),
			want:  `{"search":"","sort":"created","dir":"desc","page":1,"per_page":25,"facets":{},"hidden":[]}`,
		},
		{
			name: "everything the request chose",
			state: parse(t, url.Values{
				"q": {"  fix  "}, "sort": {"title"}, "dir": {"desc"}, "page": {"3"}, "per_page": {"10"},
				"status": {"done", "todo"}, "prio": {"high"}, "hide": {"status", "title"},
			}.Encode()),
			want: `{"search":"fix","sort":"title","dir":"desc","page":3,"per_page":10,` +
				`"facets":{"priority":["high"],"status":["todo","done"]},"hidden":["title","status"]}`,
		},
		{
			name:  "a facet with nothing selected is absent",
			state: parse(t, "status=todo").ClearFacet("status"),
			want:  `{"search":"","sort":"created","dir":"desc","page":1,"per_page":25,"facets":{},"hidden":[]}`,
		},
		{
			name:  "the zero state belongs to no listing and still encodes",
			state: pagination.ListingState{},
			want:  `{"search":"","sort":"","dir":"asc","page":1,"per_page":0,"facets":{},"hidden":[]}`,
		},
	}
	for _, c := range cases {
		if got := marshal(t, c.state); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}

	// By value or by pointer, direct or inside a struct, it is the same text.
	state := parse(t, "q=a&page=2")
	if marshal(t, state) != marshal(t, &state) || marshal(t, struct{ S pagination.ListingState }{state}) != `{"S":`+marshal(t, state)+`}` {
		t.Errorf("the state encodes differently by value, by pointer and inside a struct")
	}
}

func TestAFacetCountEncodesWithItsValueItsCountAndWhetherItIsSelected(t *testing.T) {
	got := marshal(t, []pagination.FacetCount{{Value: "done", Count: 5, Selected: true}, {Value: "todo"}})
	want := `[{"value":"done","count":5,"selected":true},{"value":"todo","count":0,"selected":false}]`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// resultOf is a result as the query builder makes one for the task listing: the
// state read from raw, 57 matching rows, a count for every value of both facets.
func resultOf(t *testing.T, raw string) *pagination.ListingResult {
	t.Helper()
	state := parse(t, raw)
	page := pagination.NewLengthAwarePage(2, 57, state.PerPage(), state.Page(), state.PageOptions("/tasks"))
	return pagination.NewListingResult(state, page, map[string][]pagination.FacetCount{
		"status": {
			{Value: "todo", Count: 12}, {Value: "in progress", Count: 3},
			{Value: "done", Count: 40, Selected: true}, {Value: "canceled"},
		},
		"priority": {{Value: "low", Count: 20}, {Value: "medium", Count: 20}, {Value: "high", Count: 17}},
	})
}

func TestTheResultEncodesTheStateThePageAndTheCountsOfEveryFacetValue(t *testing.T) {
	result := resultOf(t, "status=done&per_page=10&page=2")

	const wantState = `{"search":"","sort":"created","dir":"desc","page":2,"per_page":10,"facets":{"status":["done"]},"hidden":[]}`
	const wantFacets = `{"priority":[{"value":"low","count":20,"selected":false},{"value":"medium","count":20,"selected":false},{"value":"high","count":17,"selected":false}],` +
		`"status":[{"value":"todo","count":12,"selected":false},{"value":"in progress","count":3,"selected":false},{"value":"done","count":40,"selected":true},{"value":"canceled","count":0,"selected":false}]}`

	pageJSON, err := result.Page.MarshalJSON()
	if err != nil {
		t.Fatalf("Page.MarshalJSON: %v", err)
	}
	want := `{"state":` + wantState + `,"page":` + string(pageJSON) + `,"facets":` + wantFacets + `}`
	if got := marshal(t, result); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	// The page is the page's own payload, and says the total of the filtered rows.
	var decoded struct {
		Page struct {
			Total   int `json:"total"`
			PerPage int `json:"per_page"`
		} `json:"page"`
	}
	if err := json.Unmarshal([]byte(marshal(t, result)), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Page.Total != 57 || decoded.Page.PerPage != 10 {
		t.Errorf("the page reads %+v, want the total 57 and 10 to a page", decoded.Page)
	}
}

func TestAResultWithNothingToSayEncodesEmptyCollectionsAndNeverNull(t *testing.T) {
	state := parse(t, "")
	page := pagination.NewLengthAwarePage(0, 0, state.PerPage(), 1, state.PageOptions("/tasks"))

	none := marshal(t, pagination.NewListingResult(state, page, nil))
	if !strings.HasSuffix(none, `,"facets":{}}`) {
		t.Errorf("a result with no facets ends %s, want an empty object", none)
	}

	empty := marshal(t, pagination.NewListingResult(state, page, map[string][]pagination.FacetCount{"status": nil}))
	if !strings.HasSuffix(empty, `,"facets":{"status":[]}}`) {
		t.Errorf("a facet with no counts ends %s, want an empty array", empty)
	}

	// A result without a page encodes the page as null instead of failing.
	withoutPage := marshal(t, pagination.ListingResult{State: state})
	if !strings.Contains(withoutPage, `,"page":null,"facets":{}}`) {
		t.Errorf("a result without a page encodes as %s", withoutPage)
	}
	var nilResult *pagination.ListingResult
	if got := marshal(t, nilResult); got != "null" {
		t.Errorf("a nil result encodes as %s, want null", got)
	}
}

func TestTheEncodingDoesNotAliasTheResultOrTheState(t *testing.T) {
	result := resultOf(t, "status=done")
	before := marshal(t, result)
	if again := marshal(t, result); again != before {
		t.Errorf("the second encoding differs:\n%s\n%s", before, again)
	}
	// Writing the counts it returns does not reach the next encoding.
	result.FacetCounts("status")[0].Count = 999
	if marshal(t, result) != before {
		t.Errorf("a caller changing a returned slice changed what is encoded")
	}
}

// What follows reads the way ayra/widget.ServerSource.Decode does: the answer of
// the route is decoded into plain values with numbers kept as numbers, a dotted
// path is followed without regard to the case of a key, and the counts are read
// from an object of arrays of {"value", "count"}. The two places where Ayra looks
// for them, "Table.Result.facets" and "Table.Result.Page.total", are the ones a
// handler that holds the result in a Table field produces.
func lookup(data any, path string) (any, bool) {
	current := data
	for _, name := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, found := object[name]
		if !found {
			for key, value := range object {
				if strings.EqualFold(key, name) {
					next, found = value, true
				}
			}
		}
		if !found {
			return nil, false
		}
		current = next
	}
	return current, true
}

func TestAViewEnvelopeCarriesTheResultAsTheNativeTableDecodesIt(t *testing.T) {
	type row struct {
		ID    string
		Title string
	}
	type table struct {
		URL    string
		Result *pagination.ListingResult
	}
	data := struct {
		Table table
		Rows  []row
	}{
		Table: table{URL: "/tasks", Result: resultOf(t, "status=done&q=fix&sort=title&dir=asc&page=2&per_page=10&hide=title")},
		Rows:  []row{{"t1", "Fix the login"}, {"t2", "Fix the logout"}},
	}

	body, err := json.Marshal(http.ViewData{View: "tasks::index", Data: data, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		View  string          `json:"view"`
		Data  json.RawMessage `json:"data"`
		Token string          `json:"token"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.View != "tasks::index" || envelope.Token != "tok" {
		t.Fatalf("the envelope is %s (%v)", body, err)
	}

	var values any
	decoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		t.Fatal(err)
	}

	total, ok := lookup(values, "Table.Result.Page.total")
	if !ok || total.(json.Number).String() != "57" {
		t.Errorf("the total is %v (found %v), want 57", total, ok)
	}

	facets, ok := lookup(values, "Table.Result.facets")
	if !ok {
		t.Fatalf("the counts are not at Table.Result.facets: %s", envelope.Data)
	}
	got := map[string][]pagination.FacetCount{}
	for key, list := range facets.(map[string]any) {
		for _, item := range list.([]any) {
			entry := item.(map[string]any)
			count, err := entry["count"].(json.Number).Int64()
			if err != nil {
				t.Fatal(err)
			}
			got[key] = append(got[key], pagination.FacetCount{
				Value: entry["value"].(string), Count: int(count), Selected: entry["selected"].(bool),
			})
		}
	}
	want := map[string][]pagination.FacetCount{
		"status": {
			{Value: "todo", Count: 12}, {Value: "in progress", Count: 3},
			{Value: "done", Count: 40, Selected: true}, {Value: "canceled"},
		},
		"priority": {{Value: "low", Count: 20}, {Value: "medium", Count: 20}, {Value: "high", Count: 17}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the native table decodes the counts as\n%+v\nwant\n%+v", got, want)
	}

	state, ok := lookup(values, "Table.Result.state")
	if !ok {
		t.Fatalf("the state is not at Table.Result.state: %s", envelope.Data)
	}
	wantState := map[string]any{
		"search": "fix", "sort": "title", "dir": "asc", "page": json.Number("2"), "per_page": json.Number("10"),
		"facets": map[string]any{"status": []any{"done"}}, "hidden": []any{"title"},
	}
	if !reflect.DeepEqual(state, wantState) {
		t.Errorf("the native table decodes the state as\n%v\nwant\n%v", state, wantState)
	}

	// The rows are where they were, beside the result.
	if rows, ok := lookup(values, "Rows"); !ok || len(rows.([]any)) != 2 {
		t.Errorf("the rows are %v", rows)
	}
}
