package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

func TestChunkRefusesAnUnorderedQuery(t *testing.T) {
	connection := &fakeConnection{}
	_, err := newTestBuilder(connection).Chunk(context.Background(), grant(), 2, func([]query.Record, int) bool { return true })
	if err == nil {
		t.Fatal("an unordered result set was chunked")
	}
	if len(connection.calls) != 0 {
		t.Fatal("the unordered chunk reached the connection")
	}
}

func TestChunkWalksByOffsetUntilAShortPage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}

	var pages [][]query.Record
	completed, err := newTestBuilder(connection).OrderBy("id").
		Chunk(context.Background(), grant(), 2, func(rows []query.Record, page int) bool {
			pages = append(pages, rows)
			return true
		})
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("the walk reported that it was stopped")
	}
	if len(pages) != 2 || len(pages[1]) != 1 {
		t.Fatalf("the walk saw %d chunks", len(pages))
	}
	if !strings.Contains(connection.calls[0].sql, "limit 2 offset 0") {
		t.Fatalf("the first chunk asked for:\n%s", connection.calls[0].sql)
	}
	if !strings.Contains(connection.calls[1].sql, "limit 2 offset 2") {
		t.Fatalf("the second chunk asked for:\n%s", connection.calls[1].sql)
	}
}

func TestChunkStopsWhenTheCallbackAnswersFalse(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}, {"id": int64(4)}},
	}}

	completed, err := newTestBuilder(connection).OrderBy("id").
		Chunk(context.Background(), grant(), 2, func([]query.Record, int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("the walk reported that it finished after being stopped")
	}
	if len(connection.calls) != 1 {
		t.Fatalf("the walk ran %d statements after being stopped on the first", len(connection.calls))
	}
}

func TestChunkHonoursAnExistingLimit(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}

	read := 0
	if _, err := newTestBuilder(connection).OrderBy("id").Limit(3).
		Chunk(context.Background(), grant(), 2, func(rows []query.Record, _ int) bool {
			read += len(rows)
			return true
		}); err != nil {
		t.Fatal(err)
	}
	if read != 3 {
		t.Fatalf("the walk read %d rows of the three it was limited to", read)
	}
	if !strings.Contains(connection.calls[1].sql, "limit 1") {
		t.Fatalf("the last chunk asked for more than the limit left:\n%s", connection.calls[1].sql)
	}
}

// TestChunkByIdPagesByKeyAndNotByOffset is the difference between the two
// chunks, and the reason ChunkById exists.
//
// The second page asks for the rows after the last id of the first, so a row
// inserted or deleted in between moves no boundary. An offset walk would ask for
// "the rows after the first two" and read a row it has already seen, or skip one
// it has not.
func TestChunkByIdPagesByKeyAndNotByOffset(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(7)}},
	}}

	var seen []int64
	completed, err := newTestBuilder(connection).
		ChunkById(context.Background(), grant(), 2, func(rows []query.Record, _ int) bool {
			for _, row := range rows {
				seen = append(seen, row["id"].(int64))
			}
			return true
		}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("the walk reported that it was stopped")
	}
	if len(seen) != 3 || seen[2] != 7 {
		t.Fatalf("the walk saw %v", seen)
	}

	first := connection.calls[0]
	if !strings.Contains(first.sql, `"id" is not null`) {
		t.Fatalf("the first page did not exclude a null key:\n%s", first.sql)
	}
	if strings.Contains(first.sql, "offset") {
		t.Fatalf("the first page walked by offset:\n%s", first.sql)
	}

	second := connection.calls[1]
	if !strings.Contains(second.sql, `"id" > ?`) {
		t.Fatalf("the second page did not read from the boundary:\n%s", second.sql)
	}
	if strings.Contains(second.sql, "offset") {
		t.Fatalf("the second page walked by offset:\n%s", second.sql)
	}
	if got := second.bindings[len(second.bindings)-1]; got != int64(2) {
		t.Fatalf("the boundary is %#v rather than the last id of the page before", got)
	}
}

// TestChunkByIdForcesItsOwnOrdering: the boundary only holds if the rows come
// back in the order it was taken from, so an ordering on the key column is
// replaced rather than added to, and any other ordering keeps its place in
// front of it.
func TestChunkByIdForcesItsOwnOrdering(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"id": int64(1)}}}}

	if _, err := newTestBuilder(connection).OrderByDesc("id").OrderBy("email").
		ChunkById(context.Background(), grant(), 2, func([]query.Record, int) bool { return true }, "", ""); err != nil {
		t.Fatal(err)
	}

	sql := connection.calls[0].sql
	if !strings.Contains(sql, `order by "email" asc, "id" asc`) {
		t.Fatalf("the walk did not force the key ordering last:\n%s", sql)
	}
	if strings.Contains(sql, `"id" desc`) {
		t.Fatalf("the walk kept an ordering that contradicts its boundary:\n%s", sql)
	}
}

func TestChunkByIdStopsWhenTheKeyIsNotInTheResult(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"email": "ana@example.com"}, {"email": "bruno@example.com"}},
	}}

	_, err := newTestBuilder(connection).Select("email").
		ChunkById(context.Background(), grant(), 2, func([]query.Record, int) bool { return true }, "", "")
	if err == nil {
		t.Fatal("the walk carried on with no boundary to walk from")
	}
	if !strings.Contains(err.Error(), "[id]") {
		t.Fatalf("the error does not name the missing column: %v", err)
	}
}

func TestChunkByIdDescWalksDown(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(9)}, {"id": int64(8)}},
		{{"id": int64(4)}},
	}}

	if _, err := newTestBuilder(connection).
		ChunkByIdDesc(context.Background(), grant(), 2, func([]query.Record, int) bool { return true }, "", ""); err != nil {
		t.Fatal(err)
	}

	second := connection.calls[1]
	if !strings.Contains(second.sql, `"id" < ?`) {
		t.Fatalf("the descending walk did not read below the boundary:\n%s", second.sql)
	}
	if !strings.Contains(second.sql, `order by "id" desc`) {
		t.Fatalf("the descending walk is not ordered downwards:\n%s", second.sql)
	}
}

func TestChunkByIdSkipsTheOffsetAfterTheFirstPage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(5)}, {"id": int64(6)}},
		{{"id": int64(7)}},
	}}

	if _, err := newTestBuilder(connection).Offset(4).
		ChunkById(context.Background(), grant(), 2, func([]query.Record, int) bool { return true }, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(connection.calls[0].sql, "offset 4") {
		t.Fatalf("the first page dropped the offset it was given:\n%s", connection.calls[0].sql)
	}
	if strings.Contains(connection.calls[1].sql, "offset 4") {
		t.Fatalf("the second page skipped rows the boundary had already skipped:\n%s", connection.calls[1].sql)
	}
}

func TestChunkByIdReadsTheBoundaryUnderItsAlias(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"key": int64(3)}}}}

	if _, err := newTestBuilder(connection).Select("id as key").
		ChunkById(context.Background(), grant(), 2, func([]query.Record, int) bool { return true }, "id", "key"); err != nil {
		t.Fatal(err)
	}
	if len(connection.calls) != 1 {
		t.Fatalf("a short page did not end the walk: %d statements", len(connection.calls))
	}
}

func TestEachCountsInsideTheChunkAndEachByIdFromTheStart(t *testing.T) {
	ctx := context.Background()

	each := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}
	var indexes []int
	if _, err := newTestBuilder(each).OrderBy("id").Each(ctx, grant(), func(_ query.Record, i int) bool {
		indexes = append(indexes, i)
		return true
	}, 2); err != nil {
		t.Fatal(err)
	}
	if len(indexes) != 3 || indexes[2] != 0 {
		t.Fatalf("each numbered the rows %v, and the PHP numbers them inside the chunk", indexes)
	}

	byID := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}
	indexes = nil
	if _, err := newTestBuilder(byID).EachById(ctx, grant(), func(_ query.Record, i int) bool {
		indexes = append(indexes, i)
		return true
	}, 2, "", ""); err != nil {
		t.Fatal(err)
	}
	if len(indexes) != 3 || indexes[2] != 2 {
		t.Fatalf("eachById numbered the rows %v, and the PHP numbers them from the start of the walk", indexes)
	}
}

func TestChunkMapCollectsWhatTheCallbackReturns(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}

	mapped, err := newTestBuilder(connection).OrderBy("id").
		ChunkMap(context.Background(), grant(), func(row query.Record) any { return row["id"] }, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 3 || mapped[2] != int64(3) {
		t.Fatalf("chunkMap answered %#v", mapped)
	}
}

func TestLazyYieldsRowUponRowAndStopsOnAShortPage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}},
	}}

	var seen []int64
	for row, err := range newTestBuilder(connection).OrderBy("id").Lazy(context.Background(), grant(), 2) {
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, row["id"].(int64))
	}
	if len(seen) != 3 {
		t.Fatalf("the lazy walk saw %v", seen)
	}
	if len(connection.calls) != 2 {
		t.Fatalf("the lazy walk ran %d statements", len(connection.calls))
	}
}

func TestLazyStopsWhenTheRangeBreaks(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(3)}, {"id": int64(4)}},
	}}

	for range newTestBuilder(connection).OrderBy("id").Lazy(context.Background(), grant(), 2) {
		break
	}
	if len(connection.calls) != 1 {
		t.Fatalf("breaking out of the walk still read %d pages", len(connection.calls))
	}
}

func TestLazyByIdWalksByKey(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"id": int64(1)}, {"id": int64(2)}},
		{{"id": int64(9)}},
	}}

	count := 0
	for _, err := range newTestBuilder(connection).LazyById(context.Background(), grant(), 2, "", "") {
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("the lazy keyset walk saw %d rows", count)
	}
	if !strings.Contains(connection.calls[1].sql, `"id" > ?`) {
		t.Fatalf("the second page did not read from the boundary:\n%s", connection.calls[1].sql)
	}
}

func TestLazyRefusesAChunkSizeBelowOne(t *testing.T) {
	connection := &fakeConnection{}
	for _, err := range newTestBuilder(connection).OrderBy("id").Lazy(context.Background(), grant(), 0) {
		if err == nil {
			t.Fatal("a chunk size of zero was accepted")
		}
	}
	if len(connection.calls) != 0 {
		t.Fatal("the walk with no chunk size reached the connection")
	}
}

func TestCursorStreamsThroughTheConnection(t *testing.T) {
	connection := &fakeConnection{streamed: []query.Record{{"id": int64(1)}, {"id": int64(2)}}}

	count := 0
	for row, err := range newTestBuilder(connection).Cursor(context.Background(), grant()) {
		if err != nil {
			t.Fatal(err)
		}
		if row["id"] == nil {
			t.Fatal("a streamed row arrived empty")
		}
		count++
	}
	if count != 2 {
		t.Fatalf("the cursor yielded %d rows", count)
	}
	if !strings.Contains(connection.lastSQL(), "tenant_id") {
		t.Fatalf("the streamed statement is not scoped:\n%s", connection.lastSQL())
	}
}

func TestCursorRefusesAConnectionThatCannotStream(t *testing.T) {
	connection := &bufferedConnection{}
	builder := query.NewBuilder(connection, &fakeGrammar{}, &fakeProcessor{}).From("users")

	var got error
	for _, err := range builder.Cursor(context.Background(), grant()) {
		got = err
	}
	if got == nil {
		t.Fatal("a connection that cannot stream was allowed to pretend")
	}
	if !strings.Contains(got.Error(), "CursorConnection") {
		t.Fatalf("the refusal does not say what is missing: %v", got)
	}
}

func TestCursorReportsTheConnectionsError(t *testing.T) {
	want := errors.New("connection went away")
	connection := &fakeConnection{cursorErr: want}

	var got error
	for _, err := range newTestBuilder(connection).Cursor(context.Background(), grant()) {
		got = err
	}
	if !errors.Is(got, want) {
		t.Fatalf("the cursor answered %v", got)
	}
}

// bufferedConnection implements query.Connection and nothing else, which is
// what a driver that cannot stream a result set looks like.
type bufferedConnection struct{}

func (c *bufferedConnection) Select(string, []any, bool) ([]query.Record, error) { return nil, nil }
func (c *bufferedConnection) Insert(string, []any) (bool, error)                 { return true, nil }
func (c *bufferedConnection) Update(string, []any) (int64, error)                { return 0, nil }
func (c *bufferedConnection) Delete(string, []any) (int64, error)                { return 0, nil }
func (c *bufferedConnection) Statement(string, []any) (bool, error)              { return true, nil }
