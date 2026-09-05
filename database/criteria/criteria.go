// Package criteria translates a query string into clauses a declared query
// accepts, and refuses everything else.
//
// A listing screen wants filtering, sorting, sparse fields and includes from
// the URL. The query engine to build them with already exists; what did not
// exist is the door, and a naive door turns ?filter[users.password]=x into SQL.
//
// # What never crosses
//
// Four things never come from the request: a table name, a column name, an
// operator and SQL. The request names a declaration -- "status", "q",
// "created" -- and the declaration, written in Go on the server, says which
// column that is and how it compares. A name nobody declared does not exist,
// which is an error the caller sees rather than a clause.
//
// The public name is deliberately not the column name. Renaming a column must
// not break a URL somebody bookmarked, and publishing column names hands out
// the schema alongside the rows.
//
// # What this package does not do
//
// It does not query. Nothing here reaches a connection, and nothing here can:
// a plan is applied to a builder, and running that builder still takes the
// Grant every read already takes. Tenant scoping is likewise untouched -- it
// belongs to the model the builder was made from, and no declaration can turn
// it off.
package criteria

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Match is how a declared filter compares, and the set is closed.
//
// It is part of the declaration and never of the request. A URL that could name
// its own comparison would be choosing an operator, which is one of the four
// things that never cross: a filter that has to compare two ways is two
// declarations, "created_after" and "created_before", and each says in its name
// what it does.
type Match uint8

const (
	// Exact is column = value.
	Exact Match = iota + 1
	// Partial is column like %value%. It is the one a search box uses.
	Partial
	// Prefix is column like value%.
	Prefix
	// Suffix is column like %value.
	Suffix
	// Above is column > value.
	Above
	// Below is column < value.
	Below
	// AtLeast is column >= value.
	AtLeast
	// AtMost is column <= value.
	AtMost
	// OneOf is column in (values), and its value is a comma-separated list.
	OneOf
)

// operator returns the SQL comparison a match makes. It is unexported because
// the spelling is between this package and the query grammar, which normalizes
// it against the dialect that will compile it -- there is no second list of
// operators here, only a mapping onto the one that exists.
func (m Match) operator() string {
	switch m {
	case Exact:
		return "="
	case Partial, Prefix, Suffix:
		return "like"
	case Above:
		return ">"
	case Below:
		return "<"
	case AtLeast:
		return ">="
	case AtMost:
		return "<="
	}
	return ""
}

// identifier is what a declared column may be spelled as: a name, or a name
// qualified by a table. It is checked at declaration and never at request time,
// because a request never carries one.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// filterSpec is one declared filter: the public name, the column it reads and
// the comparison it makes.
type filterSpec struct {
	column string
	match  Match
}

// Declaration is the allowlist: the filters, sorts, fields and includes a
// listing accepts, and the page size it will not exceed.
//
// It is built once, at registration, and read on every request. Building it is
// the moment a mistake is worth finding, so a duplicate name, an empty column
// or a column that is not an identifier panics here rather than becoming a
// clause nobody can explain later.
type Declaration struct {
	filters  map[string]filterSpec
	sorts    map[string]string
	fields   map[string]string
	includes map[string]string

	perPage int
	maxPage int
}

// Declare returns an empty declaration: nothing is filterable, sortable,
// selectable or includable until it is named.
//
// The default page size is 15 and the ceiling is 100. Page replaces both.
func Declare() *Declaration {
	return &Declaration{
		filters:  map[string]filterSpec{},
		sorts:    map[string]string{},
		fields:   map[string]string{},
		includes: map[string]string{},
		perPage:  15,
		maxPage:  100,
	}
}

// Filter declares that name filters on column with the given comparison.
//
//	Declare().
//		Filter("status", "status", criteria.Exact).
//		Filter("q", "name", criteria.Partial).
//		Filter("created_after", "created_at", criteria.Above)
func (d *Declaration) Filter(name, column string, match Match) *Declaration {
	d.claim("filter", name, column)
	if match == 0 || match > OneOf {
		panic(fmt.Sprintf("criteria: filter %q was declared with no comparison", name))
	}
	d.filters[name] = filterSpec{column: column, match: match}
	return d
}

// Sort declares that name orders by column, in either direction.
func (d *Declaration) Sort(name, column string) *Declaration {
	d.claim("sort", name, column)
	d.sorts[name] = column
	return d
}

// Field declares that name may be asked for on its own, reading column.
//
// A request that asks for fields gets those columns and no others, so a
// declaration whose fields leave out the primary key hands back rows the model
// layer cannot key -- declare the key among them.
func (d *Declaration) Field(name, column string) *Declaration {
	d.claim("field", name, column)
	d.fields[name] = column
	return d
}

// Include declares that name eager-loads the given relation.
//
// The relation is the name the model knows it by, which is why it is not
// checked as a column: it never reaches SQL as an identifier, it reaches the
// builder as a relation to resolve.
func (d *Declaration) Include(name, relation string) *Declaration {
	if name == "" {
		panic("criteria: an include was declared with no name")
	}
	if relation == "" {
		panic(fmt.Sprintf("criteria: include %q was declared with no relation", name))
	}
	if _, taken := d.includes[name]; taken {
		panic(fmt.Sprintf("criteria: include %q was declared twice", name))
	}
	d.includes[name] = relation
	return d
}

// Page sets the default page size and the ceiling a request may not raise.
//
// The ceiling is the point of the pair: without one, per_page=1000000 is a
// table scan anybody can ask for.
func (d *Declaration) Page(size, max int) *Declaration {
	if size < 1 || max < 1 || size > max {
		panic(fmt.Sprintf("criteria: page sizes have to be positive and the default no larger than the ceiling, got %d and %d", size, max))
	}
	d.perPage = size
	d.maxPage = max
	return d
}

// claim refuses a name or a column that could not have been meant, at the
// moment it is written.
func (d *Declaration) claim(kind, name, column string) {
	if name == "" {
		panic("criteria: a " + kind + " was declared with no name")
	}
	if !identifier.MatchString(column) {
		panic(fmt.Sprintf("criteria: %s %q reads %q, which is not a column name", kind, name, column))
	}
	_, taken := false, false
	switch kind {
	case "filter":
		_, taken = d.filters[name]
	case "sort":
		_, taken = d.sorts[name]
	case "field":
		_, taken = d.fields[name]
	}
	if taken {
		panic(fmt.Sprintf("criteria: %s %q was declared twice", kind, name))
	}
}

// Clause is one filter, translated: the column it reads, how it compares and
// the value it compares against.
//
// Value is a string for every match but OneOf, whose value is the list.
type Clause struct {
	// Name is the public name the request used, kept so an error can name what
	// the caller wrote rather than what it was mapped to.
	Name string
	// Column is the declared column.
	Column string
	// Match is the declared comparison.
	Match Match
	// Value is what to compare against: a string, or []string for OneOf.
	Value any
}

// Order is one sort, translated.
type Order struct {
	// Name is the public name the request used.
	Name string
	// Column is the declared column.
	Column string
	// Descending is what the leading "-" asked for.
	Descending bool
}

// Plan is a request, translated. It holds no SQL and no request.
type Plan struct {
	// Filters are the clauses to add, in the order the declaration was read.
	Filters []Clause
	// Orders are the sorts to add, in the order the request wrote them.
	Orders []Order
	// Columns are the declared columns of the fields asked for, empty when the
	// request asked for none.
	Columns []string
	// Includes are the declared relations to eager-load.
	Includes []string
	// Page is the page number asked for, at least 1.
	Page int
	// PerPage is the page size, already held under the declared ceiling.
	//
	// It is carried rather than applied, because applying it would be a second
	// way to page: the paginator takes the size and the number, counts the
	// total and returns the page. A plan that had already put a limit on the
	// builder would make it count a page instead of a table.
	PerPage int
}

// Error is a request this declaration refuses, and it is the only error Parse
// returns. Parameter is the query-string parameter as the caller wrote it, so
// the answer can point at what to change.
type Error struct {
	Parameter string
	Reason    string
}

func (e *Error) Error() string { return "criteria: " + e.Parameter + ": " + e.Reason }

// Parse translates a query string against the declaration.
//
// Anything the declaration does not name is an error, including a filter that
// exists on the table: the allowlist is the whole of what is reachable, and a
// column nobody declared is not reachable by spelling it.
//
// A parameter the vocabulary does not use at all -- a tracking parameter, a
// cache buster -- is ignored, because it is not addressed to this.
func (d *Declaration) Parse(values url.Values) (Plan, error) {
	plan := Plan{Page: 1, PerPage: d.perPage}

	for key, raw := range values {
		name, ok := filterName(key)
		if !ok {
			continue
		}
		spec, declared := d.filters[name]
		if !declared {
			return Plan{}, &Error{Parameter: key, Reason: "no filter is declared under that name"}
		}

		if spec.match == OneOf {
			list := splitList(raw)
			if len(list) == 0 {
				continue
			}
			plan.Filters = append(plan.Filters, Clause{Name: name, Column: spec.column, Match: spec.match, Value: list})
			continue
		}

		if len(raw) > 1 {
			return Plan{}, &Error{Parameter: key, Reason: "was given more than once, and this filter compares a single value"}
		}
		value := strings.TrimSpace(raw[0])
		if value == "" {
			continue
		}
		plan.Filters = append(plan.Filters, Clause{Name: name, Column: spec.column, Match: spec.match, Value: value})
	}

	// Filters come out of a map, so they are ordered here rather than left to
	// the range: the same URL has to produce the same SQL, or a query cache and
	// a test both have a different answer every run.
	sortClauses(plan.Filters)

	for _, name := range splitList(values["sort"]) {
		descending := strings.HasPrefix(name, "-")
		name = strings.TrimPrefix(name, "-")
		column, declared := d.sorts[name]
		if !declared {
			return Plan{}, &Error{Parameter: "sort", Reason: fmt.Sprintf("no sort is declared under %q", name)}
		}
		plan.Orders = append(plan.Orders, Order{Name: name, Column: column, Descending: descending})
	}

	for _, name := range splitList(values["fields"]) {
		column, declared := d.fields[name]
		if !declared {
			return Plan{}, &Error{Parameter: "fields", Reason: fmt.Sprintf("no field is declared under %q", name)}
		}
		plan.Columns = append(plan.Columns, column)
	}

	for _, name := range splitList(values["include"]) {
		relation, declared := d.includes[name]
		if !declared {
			return Plan{}, &Error{Parameter: "include", Reason: fmt.Sprintf("no include is declared under %q", name)}
		}
		plan.Includes = append(plan.Includes, relation)
	}

	page, err := readNumber(values, "page")
	if err != nil {
		return Plan{}, err
	}
	if page > 0 {
		plan.Page = page
	}

	perPage, err := readNumber(values, "per_page")
	if err != nil {
		return Plan{}, err
	}
	if perPage > 0 {
		if perPage > d.maxPage {
			return Plan{}, &Error{
				Parameter: "per_page",
				Reason:    fmt.Sprintf("%d is above the %d this listing allows", perPage, d.maxPage),
			}
		}
		plan.PerPage = perPage
	}

	return plan, nil
}

// filterName reads "filter[status]" and answers "status".
func filterName(key string) (string, bool) {
	if !strings.HasPrefix(key, "filter[") || !strings.HasSuffix(key, "]") {
		return "", false
	}
	name := key[len("filter[") : len(key)-1]
	if name == "" || strings.ContainsAny(name, "[]") {
		return "", false
	}
	return name, true
}

// splitList reads a repeated or comma-separated parameter as a list, dropping
// the empty members a form sends when a control was left alone.
func splitList(raw []string) []string {
	var out []string
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// readNumber reads a positive whole number, and refuses anything else rather
// than reading it as zero.
func readNumber(values url.Values, key string) (int, error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, &Error{Parameter: key, Reason: fmt.Sprintf("%q is not a page number", raw)}
	}
	return n, nil
}

// sortClauses orders the clauses by public name, which is the one ordering both
// ends of a request agree on.
func sortClauses(clauses []Clause) {
	for i := 1; i < len(clauses); i++ {
		for j := i; j > 0 && clauses[j].Name < clauses[j-1].Name; j-- {
			clauses[j], clauses[j-1] = clauses[j-1], clauses[j]
		}
	}
}
