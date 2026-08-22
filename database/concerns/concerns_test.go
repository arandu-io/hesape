package concerns_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"

	"github.com/arandu-io/hesape/database/concerns"
)

// rows is a Chunkable over a slice, which is enough to test the arithmetic that
// BuildsQueries is: the offsets, the limits and the stopping conditions.
type rows struct {
	all      []int
	offset   *int
	limit    *int
	ordered  bool
	pages    int
	keyName  string
	afterID  any
	beforeID any
}

func (r *rows) GetOffset() *int { return r.offset }
func (r *rows) GetLimit() *int  { return r.limit }
func (r *rows) Offset(v int)    { r.offset = &v }
func (r *rows) Limit(v int)     { r.limit = &v }

func (r *rows) ForPage(page, perPage int) {
	r.Offset((page - 1) * perPage)
	r.Limit(perPage)
}

func (r *rows) EnforceOrderBy() error {
	if !r.ordered {
		return errors.New("You must specify an orderBy clause when using this function.")
	}
	return nil
}

func (r *rows) Get(context.Context, auth.Grant) ([]int, error) {
	r.pages++

	start := 0
	if r.offset != nil {
		start = *r.offset
	}
	if start > len(r.all) {
		return nil, nil
	}

	end := len(r.all)
	if r.limit != nil && start+*r.limit < end {
		end = start + *r.limit
	}
	return r.all[start:end], nil
}

func (r *rows) Clone() concerns.KeyChunkable[int] {
	clone := *r
	return &clone
}

func (r *rows) DefaultKeyName() string { return "id" }

func (r *rows) ForPageAfterID(perPage int, lastID any, _ string) {
	r.afterID = lastID
	start := 0
	if lastID != nil {
		start = lastID.(int)
	}
	r.Offset(start)
	r.Limit(perPage)
}

func (r *rows) ForPageBeforeID(perPage int, lastID any, _ string) {
	r.beforeID = lastID
	r.Limit(perPage)
}

func (r *rows) ValueOf(row int, _ string) (any, bool) { return row, true }

func TestChunkRefusesAnUnorderedQuery(t *testing.T) {
	source := &rows{all: []int{1, 2, 3}}

	_, err := concerns.Chunk(context.Background(), auth.Grant{}, source, 2, func([]int, int) bool { return true })
	if err == nil {
		t.Fatal("Chunk walked an unordered query, which repeats rows and skips others")
	}
}

func TestChunkWalksEveryRowOnce(t *testing.T) {
	source := &rows{all: []int{1, 2, 3, 4, 5}, ordered: true}

	var seen []int
	ok, err := concerns.Chunk(context.Background(), auth.Grant{}, source, 2, func(page []int, _ int) bool {
		seen = append(seen, page...)
		return true
	})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if !ok {
		t.Fatal("Chunk answered false, and nothing stopped it")
	}
	if len(seen) != 5 {
		t.Fatalf("Chunk saw %v, want all five rows exactly once", seen)
	}
}

func TestChunkStopsWhenTheCallbackAnswersFalse(t *testing.T) {
	source := &rows{all: []int{1, 2, 3, 4, 5, 6}, ordered: true}

	pages := 0
	ok, err := concerns.Chunk(context.Background(), auth.Grant{}, source, 2, func([]int, int) bool {
		pages++
		return false
	})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if ok {
		t.Fatal("Chunk answered true after the callback stopped it")
	}
	if pages != 1 {
		t.Fatalf("the callback ran %d times after answering false once", pages)
	}
}

func TestChunkHonoursAnExistingLimit(t *testing.T) {
	source := &rows{all: []int{1, 2, 3, 4, 5, 6, 7, 8}, ordered: true}
	source.Limit(3)

	var seen []int
	if _, err := concerns.Chunk(context.Background(), auth.Grant{}, source, 2, func(page []int, _ int) bool {
		seen = append(seen, page...)
		return true
	}); err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("Chunk saw %d rows through a limit of 3", len(seen))
	}
}

func TestFirstAndSole(t *testing.T) {
	ctx, g := context.Background(), auth.Grant{}

	one, found, err := concerns.First(ctx, g, &rows{all: []int{7, 8}, ordered: true})
	if err != nil || !found || one != 7 {
		t.Fatalf("First = (%v, %v, %v), want (7, true, nil)", one, found, err)
	}

	if _, found, _ := concerns.First(ctx, g, &rows{ordered: true}); found {
		t.Fatal("First found a row in an empty result")
	}

	if _, err := concerns.FirstOrFail(ctx, g, &rows{ordered: true}, ""); !errors.Is(err, concerns.ErrRecordNotFound) {
		t.Fatalf("FirstOrFail answered %v, want ErrRecordNotFound", err)
	}

	if _, err := concerns.Sole(ctx, g, &rows{ordered: true}); !errors.Is(err, concerns.ErrRecordsNotFound) {
		t.Fatalf("Sole on nothing answered %v, want ErrRecordsNotFound", err)
	}

	var many *concerns.MultipleRecordsFoundError
	if _, err := concerns.Sole(ctx, g, &rows{all: []int{1, 2, 3}, ordered: true}); !errors.As(err, &many) {
		t.Fatalf("Sole on three rows answered %v, want MultipleRecordsFoundError", err)
	} else if many.GetCount() != 2 {
		t.Fatalf("Sole reported %d records; it reads two because that is all it fetched", many.GetCount())
	}
}

func TestLazyYieldsEveryRowAndStops(t *testing.T) {
	source := &rows{all: []int{1, 2, 3, 4, 5}, ordered: true}

	var seen []int
	for row, err := range concerns.Lazy(context.Background(), auth.Grant{}, source, 2) {
		if err != nil {
			t.Fatalf("Lazy: %v", err)
		}
		seen = append(seen, row)
	}
	if len(seen) != 5 {
		t.Fatalf("Lazy yielded %v, want all five", seen)
	}
}

func TestLazyRefusesAChunkSizeBelowOne(t *testing.T) {
	got := false
	for _, err := range concerns.Lazy(context.Background(), auth.Grant{}, &rows{ordered: true}, 0) {
		if err == nil {
			t.Fatal("Lazy yielded a row for a chunk size of zero")
		}
		got = true
	}
	if !got {
		t.Fatal("Lazy said nothing about a chunk size of zero")
	}
}

func TestTapAndPipe(t *testing.T) {
	source := &rows{}

	if concerns.Tap(source, func(r *rows) { r.ordered = true }) != source {
		t.Fatal("Tap answered something other than what it was given")
	}
	if !source.ordered {
		t.Fatal("Tap did not run the callback")
	}
	if concerns.Pipe(source, func(r *rows) int { return len(r.all) }) != 0 {
		t.Fatal("Pipe did not answer what the callback returned")
	}
}

func TestWrapJSONPath(t *testing.T) {
	for value, want := range map[string]string{
		"language":       `'$."language"'`,
		"language->code": `'$."language"."code"'`,
		"[0]->name":      `'$[0]."name"'`,
		"tags[0]":        `'$."tags"[0]'`,
	} {
		if got := concerns.WrapJSONPath(value, "->"); got != want {
			t.Fatalf("WrapJSONPath(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestWrapJSONFieldAndPath(t *testing.T) {
	wrap := func(v any) string { return `"` + v.(string) + `"` }

	field, path := concerns.WrapJSONFieldAndPath(wrap, "options->language")
	if field != `"options"` {
		t.Fatalf("field = %q", field)
	}
	if path != `, '$."language"'` {
		t.Fatalf("path = %q", path)
	}

	field, path = concerns.WrapJSONFieldAndPath(wrap, "options")
	if field != `"options"` || path != "" {
		t.Fatalf("a column with no path gave (%q, %q)", field, path)
	}
}

func TestParseSearchPath(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{"public", "public"},
		{"public,app", "public,app"},
		{`"public", 'app'`, "public,app"},
		{[]string{"'public'", "app"}, "public,app"},
		{nil, ""},
	} {
		if got := strings.Join(concerns.ParseSearchPath(tc.in), ","); got != tc.want {
			t.Fatalf("ParseSearchPath(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWhereTodayUsesTheDatePartAndBinds(t *testing.T) {
	frozen := time.Date(2026, 8, 11, 13, 45, 0, 0, time.UTC)
	concerns.Now = func() time.Time { return frozen }
	t.Cleanup(func() { concerns.Now = time.Now })

	b := query.NewBuilder(nil, nil, nil)
	concerns.WhereToday(b, "", "published_at")

	if len(b.Wheres) != 1 {
		t.Fatalf("WhereToday added %d clauses, want 1", len(b.Wheres))
	}
	where := b.Wheres[0]
	if where.Type != "Date" || where.Operator != "=" || where.Boolean != "and" {
		t.Fatalf("WhereToday added %+v", where)
	}
	if where.Value != "2026-08-11" {
		t.Fatalf("WhereToday bound %v, want the date part only", where.Value)
	}
	if got := b.GetBindings(); len(got) != 1 || got[0] != "2026-08-11" {
		t.Fatalf("bindings are %v", got)
	}
}

func TestWherePastBindsOnePerColumn(t *testing.T) {
	frozen := time.Date(2026, 8, 11, 13, 45, 0, 0, time.UTC)
	concerns.Now = func() time.Time { return frozen }
	t.Cleanup(func() { concerns.Now = time.Now })

	b := query.NewBuilder(nil, nil, nil)
	concerns.WherePast(b, "starts_at", "ends_at")

	if len(b.Wheres) != 2 {
		t.Fatalf("WherePast added %d clauses for two columns", len(b.Wheres))
	}
	if len(b.GetBindings()) != 2 {
		t.Fatalf("WherePast bound %d values for two columns", len(b.GetBindings()))
	}
	if b.Wheres[0].Operator != "<" {
		t.Fatalf("WherePast used %q, want <", b.Wheres[0].Operator)
	}
	if b.Wheres[0].Type != "Basic" {
		t.Fatalf("WherePast added a %s clause, want Basic", b.Wheres[0].Type)
	}
}

func TestOrWhereTodayIsAnOrClause(t *testing.T) {
	b := query.NewBuilder(nil, nil, nil)
	concerns.OrWhereToday(b, "published_at")

	if b.Wheres[0].Boolean != "or" {
		t.Fatalf("OrWhereToday added a %q clause", b.Wheres[0].Boolean)
	}
}
