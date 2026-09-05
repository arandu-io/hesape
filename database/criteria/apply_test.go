package criteria_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/criteria"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// invoice stands in for an entity, so that the builder the application actually
// uses can be named here.
type invoice struct{}

// The model builder satisfies the target as written, with no adapter and no
// method added for it. A signature that drifted would fail to compile here
// rather than at the first listing screen.
var _ criteria.Target[*model.Builder[invoice]] = (*model.Builder[invoice])(nil)

// recorder is a Target that writes down what it was asked for, so the
// translation can be read without a database.
type recorder struct{ calls []string }

func (r *recorder) Where(column any, args ...any) *recorder {
	r.calls = append(r.calls, fmt.Sprintf("where(%v, %v)", column, args))
	return r
}

func (r *recorder) WhereIn(column any, values []any) *recorder {
	r.calls = append(r.calls, fmt.Sprintf("wherein(%v, %v)", column, values))
	return r
}

func (r *recorder) OrderBy(column any, direction ...string) *recorder {
	r.calls = append(r.calls, fmt.Sprintf("orderby(%v, %v)", column, direction))
	return r
}

func (r *recorder) Select(columns ...any) *recorder {
	r.calls = append(r.calls, fmt.Sprintf("select(%v)", columns))
	return r
}

func (r *recorder) With(relations ...string) *recorder {
	r.calls = append(r.calls, fmt.Sprintf("with(%v)", relations))
	return r
}

func applied(t *testing.T, d *criteria.Declaration, raw string) []string {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q) = %v", raw, err)
	}
	plan, err := d.Parse(values)
	if err != nil {
		t.Fatalf("Parse(%q) = %v", raw, err)
	}
	return criteria.Apply(&recorder{}, plan).calls
}

func TestApplyTranslatesEveryMatch(t *testing.T) {
	d := criteria.Declare().
		Filter("exact", "a", criteria.Exact).
		Filter("partial", "b", criteria.Partial).
		Filter("prefix", "c", criteria.Prefix).
		Filter("suffix", "d", criteria.Suffix).
		Filter("above", "e", criteria.Above).
		Filter("below", "f", criteria.Below).
		Filter("atleast", "g", criteria.AtLeast).
		Filter("atmost", "h", criteria.AtMost).
		Filter("oneof", "i", criteria.OneOf)

	for _, c := range []struct{ raw, want string }{
		{"filter[exact]=x", "where(a, [= x])"},
		{"filter[partial]=x", "where(b, [like %x%])"},
		{"filter[prefix]=x", "where(c, [like x%])"},
		{"filter[suffix]=x", "where(d, [like %x])"},
		{"filter[above]=x", "where(e, [> x])"},
		{"filter[below]=x", "where(f, [< x])"},
		{"filter[atleast]=x", "where(g, [>= x])"},
		{"filter[atmost]=x", "where(h, [<= x])"},
		{"filter[oneof]=x,y", "wherein(i, [x y])"},
	} {
		t.Run(c.raw, func(t *testing.T) {
			got := applied(t, d, c.raw)
			if len(got) != 1 || got[0] != c.want {
				t.Errorf("got %v, want [%s]", got, c.want)
			}
		})
	}
}

func TestApplyPassesTheDeclaredColumnAndNotThePublicName(t *testing.T) {
	got := applied(t, listing(), "filter[q]=ana&sort=-created&fields=id,name&include=author")

	want := []string{
		"where(name, [like %ana%])",
		"orderby(created_at, [desc])",
		"select([id name])",
		"with([Author])",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestApplyAddsNoPage(t *testing.T) {
	// The page belongs to the paginator, which needs the size to count the
	// total with. A limit added here would be a second way to page.
	got := applied(t, listing(), "page=3&per_page=20")
	if len(got) != 0 {
		t.Errorf("a page became a clause: %v", got)
	}
}

func TestAValueCannotCarryItsOwnPattern(t *testing.T) {
	d := criteria.Declare().Filter("q", "name", criteria.Partial)

	got := applied(t, d, "filter[q]="+url.QueryEscape("100%_a\\b"))
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	for _, wildcard := range []string{`\%`, `\_`, `\\`} {
		if !strings.Contains(got[0], wildcard) {
			t.Errorf("%q does not escape %s", got[0], wildcard)
		}
	}
}

// base is a Target over the real query builder, so that what a plan produces
// can be compiled by the grammar that would compile it in a request. The base
// builder has no relations, so With does nothing and no test here asks it to.
type base struct{ b *query.Builder }

func (t base) Where(column any, args ...any) base { t.b.Where(column, args...); return t }

func (t base) WhereIn(column any, values []any) base { t.b.WhereIn(column, values); return t }

func (t base) OrderBy(column any, direction ...string) base {
	t.b.OrderBy(column, direction...)
	return t
}

func (t base) Select(columns ...any) base { t.b.Select(columns...); return t }

func (t base) With(relations ...string) base { return t }

// TestEveryDeclaredComparisonIsOneTheGrammarAccepts. The operators are not a
// second allowlist: they are spellings the query grammar already normalizes,
// and an operator it does not know sets an error on the builder rather than
// reaching the statement.
func TestEveryDeclaredComparisonIsOneTheGrammarAccepts(t *testing.T) {
	d := criteria.Declare().
		Filter("exact", "a", criteria.Exact).
		Filter("partial", "b", criteria.Partial).
		Filter("above", "c", criteria.Above).
		Filter("below", "d", criteria.Below).
		Filter("atleast", "e", criteria.AtLeast).
		Filter("atmost", "f", criteria.AtMost).
		Filter("oneof", "g", criteria.OneOf).
		Sort("name", "name")

	values, err := url.ParseQuery("filter[exact]=1&filter[partial]=x&filter[above]=1&filter[below]=1&filter[atleast]=1&filter[atmost]=1&filter[oneof]=a,b&sort=-name")
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	plan, err := d.Parse(values)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	builder := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil)
	builder.From("invoices")
	criteria.Apply(base{b: builder}, plan)

	if err := builder.Err(); err != nil {
		t.Fatalf("the grammar refused a declared comparison: %v", err)
	}
	// The grammar spells its own dialect: PostgreSQL casts a like operand, so
	// the operator is what is asserted and not the whole clause.
	sql := builder.ToSQL()
	for _, want := range []string{`"a" = ?`, `like ?`, `"c" > ?`, `"d" < ?`, `"e" >= ?`, `"f" <= ?`, `"g" in (?, ?)`, `order by "name" desc`} {
		if !strings.Contains(sql, want) {
			t.Errorf("%s\ndoes not contain %s", sql, want)
		}
	}
}
