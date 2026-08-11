package query

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/arandu-io/hesape/auth"
)

// The chunking half of Illuminate\Database\Concerns\BuildsQueries, and the two
// methods of Builder that keyset chunking is built on.
//
// Every statement issued here goes through Get, so every chunk carries the
// tenant of the Grant. A walk that filtered on the first page and not on the
// rest would be a leak that only shows up on a table big enough to need
// chunking, which is every table that matters.

// errRequiresOrderBy answers the RuntimeException that
// Builder::enforceOrderBy throws.
var errRequiresOrderBy = errors.New("query: you must specify an orderBy clause when using this function")

// enforceOrderBy answers Builder::enforceOrderBy.
//
// Chunking and lazy walking both read the result set in pieces, and a result
// set with no ordering has no pieces: the engine is free to return the rows in
// a different order for each statement, so a row can appear on two chunks and
// never on the third.
func (b *Builder) enforceOrderBy() error {
	if len(b.Orders) == 0 && len(b.UnionOrders) == 0 {
		return errRequiresOrderBy
	}
	return nil
}

// defaultKeyName answers Builder::defaultKeyName, which is protected there.
func (b *Builder) defaultKeyName() string { return "id" }

// ForPageAfterId answers Builder::forPageAfterId: the next page of results
// after a given id.
//
// The existing ordering on that column is dropped and the column is ordered by
// again, last: the boundary is only a boundary if the rows come back in the
// order the boundary was taken from. Other orderings are kept, and the id
// becomes their tiebreaker.
//
// A nil lastId is the first page, and asks for the column not to be null --
// a null id can never be compared past, so a row holding one would be returned
// by every page.
func (b *Builder) ForPageAfterId(perPage int, lastId any, column string) *Builder {
	if column == "" {
		column = b.defaultKeyName()
	}
	b.Orders = b.removeExistingOrdersFor(column)

	if lastId == nil {
		b.WhereNotNull(column)
	} else {
		b.Where(column, ">", lastId)
	}
	return b.OrderBy(column, "asc").Limit(perPage)
}

// ForPageBeforeId answers Builder::forPageBeforeId: the previous page of
// results before a given id. See ForPageAfterId.
func (b *Builder) ForPageBeforeId(perPage int, lastId any, column string) *Builder {
	if column == "" {
		column = b.defaultKeyName()
	}
	b.Orders = b.removeExistingOrdersFor(column)

	if lastId == nil {
		b.WhereNotNull(column)
	} else {
		b.Where(column, "<", lastId)
	}
	return b.OrderBy(column, "desc").Limit(perPage)
}

// removeExistingOrdersFor answers Builder::removeExistingOrdersFor.
//
// The PHP compares the column with ===, which for two strings is what comparing
// their text is. An Expression is compared by the SQL it carries rather than by
// identity, because two Expressions holding the same text order by the same
// thing and keeping both would order by it twice.
func (b *Builder) removeExistingOrdersFor(column string) []Order {
	out := make([]Order, 0, len(b.Orders))
	for _, order := range b.Orders {
		if order.Column != nil && stringify(order.Column) == column {
			continue
		}
		out = append(out, order)
	}
	return out
}

// Chunk answers Concerns\BuildsQueries::chunk.
//
// It walks the result set by offset, a page at a time, and stops when the
// callback answers false -- which is the PHP's `=== false`, written as a bool
// because Go has one.
//
// This is the chunking that skips rows. Between two pages the table can change,
// and every insert before the offset shifts a row past the boundary that was
// already read. ChunkById does not have that failure, and is what to reach for
// on a table that is being written to.
//
// A limit or an offset already on the query is honoured: the offset is where
// the walk starts, and the limit is how many rows it reads in total.
func (b *Builder) Chunk(ctx context.Context, g auth.Grant, count int, callback func(rows []Record, page int) bool) (bool, error) {
	if err := b.enforceOrderBy(); err != nil {
		return false, err
	}

	skip := 0
	if offset := b.GetOffset(); offset != nil {
		skip = *offset
	}
	remaining := b.GetLimit()
	page := 1

	for {
		offset := (page-1)*count + skip

		limit := count
		if remaining != nil {
			limit = min(count, *remaining)
		}
		if limit == 0 {
			return true, nil
		}

		rows, err := b.Offset(offset).Limit(limit).Get(ctx, g)
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return true, nil
		}
		if remaining != nil {
			left := max(*remaining-len(rows), 0)
			remaining = &left
		}
		if callback != nil && !callback(rows, page) {
			return false, nil
		}
		if len(rows) != count {
			return true, nil
		}
		page++
	}
}

// ChunkMap answers Concerns\BuildsQueries::chunkMap.
//
// The PHP's TReturn is any here: a method in Go cannot introduce a type
// parameter of its own, and making this a function instead of a method would
// take it out of the chain it is written in.
func (b *Builder) ChunkMap(ctx context.Context, g auth.Grant, callback func(row Record) any, count int) ([]any, error) {
	if count < 1 {
		count = 1000
	}
	out := make([]any, 0)
	_, err := b.Chunk(ctx, g, count, func(rows []Record, _ int) bool {
		for _, row := range rows {
			out = append(out, callback(row))
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Each answers Concerns\BuildsQueries::each.
//
// The index is the row's position inside its chunk, which is what the PHP's
// `foreach ($results as $key => $value)` yields over a fresh Collection per
// chunk. EachById counts from the start of the walk instead, and the difference
// is deliberate in both.
func (b *Builder) Each(ctx context.Context, g auth.Grant, callback func(row Record, index int) bool, count int) (bool, error) {
	if count < 1 {
		count = 1000
	}
	return b.Chunk(ctx, g, count, func(rows []Record, _ int) bool {
		for i, row := range rows {
			if !callback(row, i) {
				return false
			}
		}
		return true
	})
}

// ChunkById answers Concerns\BuildsQueries::chunkById.
//
// This is the chunk that does not skip rows. Instead of counting rows to reach
// page n, each page asks for the rows after the last id of the page before it,
// so an insert or a delete anywhere in the table moves no boundary. It is the
// one to use for anything that walks a table while the application is running.
//
// An empty column means the id. alias is the name that column comes back under
// when the select renames it -- give it whenever the query selects the key
// column under another name, because the walk reads the boundary out of the row
// by that name, and a boundary it cannot read stops the walk with an error
// rather than silently repeating the first page forever.
func (b *Builder) ChunkById(ctx context.Context, g auth.Grant, count int, callback func(rows []Record, page int) bool, column, alias string) (bool, error) {
	return b.OrderedChunkById(ctx, g, count, callback, column, alias, false)
}

// ChunkByIdDesc answers Concerns\BuildsQueries::chunkByIdDesc: ChunkById
// walking from the highest id down.
func (b *Builder) ChunkByIdDesc(ctx context.Context, g auth.Grant, count int, callback func(rows []Record, page int) bool, column, alias string) (bool, error) {
	return b.OrderedChunkById(ctx, g, count, callback, column, alias, true)
}

// OrderedChunkById answers Concerns\BuildsQueries::orderedChunkById.
//
// Each page runs on a copy of the query, because ForPageAfterId adds a where
// and rewrites the ordering: applying that to the query itself would leave the
// previous page's boundary on the next page's statement, and the walk would
// stand still.
//
// An offset already on the query applies to the first page only. After that the
// id boundary is doing the skipping, and skipping again would drop a page's
// worth of rows on every page.
func (b *Builder) OrderedChunkById(ctx context.Context, g auth.Grant, count int, callback func(rows []Record, page int) bool, column, alias string, descending bool) (bool, error) {
	if column == "" {
		column = b.defaultKeyName()
	}
	if alias == "" {
		alias = column
	}

	var lastId any
	skip := 0
	if offset := b.GetOffset(); offset != nil {
		skip = *offset
	}
	remaining := b.GetLimit()
	page := 1

	for {
		clone := b.Clone()
		if skip != 0 && page > 1 {
			clone.Offset(0)
		}

		limit := count
		if remaining != nil {
			limit = min(count, *remaining)
		}
		if limit == 0 {
			return true, nil
		}

		if descending {
			clone.ForPageBeforeId(limit, lastId, column)
		} else {
			clone.ForPageAfterId(limit, lastId, column)
		}

		rows, err := clone.Get(ctx, g)
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return true, nil
		}
		if remaining != nil {
			left := max(*remaining-len(rows), 0)
			remaining = &left
		}
		if callback != nil && !callback(rows, page) {
			return false, nil
		}

		lastId = rows[len(rows)-1][alias]
		if lastId == nil {
			return false, fmt.Errorf("query: the chunkById operation was aborted because the [%s] column is not present in the query result", alias)
		}
		if len(rows) != count {
			return true, nil
		}
		page++
	}
}

// EachById answers Concerns\BuildsQueries::eachById.
//
// The index counts from the start of the walk, not from the start of the chunk:
// it is the PHP's (($page - 1) * $count) + $key.
func (b *Builder) EachById(ctx context.Context, g auth.Grant, callback func(row Record, index int) bool, count int, column, alias string) (bool, error) {
	if count < 1 {
		count = 1000
	}
	return b.ChunkById(ctx, g, count, func(rows []Record, page int) bool {
		for i, row := range rows {
			if !callback(row, (page-1)*count+i) {
				return false
			}
		}
		return true
	}, column, alias)
}

// Lazy answers Concerns\BuildsQueries::lazy.
//
// The PHP returns a LazyCollection, which is a closure producing a generator. A
// Go iter.Seq2 is that closure. The second value is the error, because a walk
// that fails halfway has nowhere else to report it and a sequence that just
// stopped early would read as "the table ended here" -- which is the same
// answer as success and is why this is not a Seq of rows.
//
// The sequence walks by offset and skips rows for the reason Chunk does. LazyById
// is the one that holds still.
func (b *Builder) Lazy(ctx context.Context, g auth.Grant, chunkSize int) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		if chunkSize < 1 {
			yield(nil, errors.New("query: the chunk size should be at least 1"))
			return
		}
		if err := b.enforceOrderBy(); err != nil {
			yield(nil, err)
			return
		}

		for page := 1; ; page++ {
			rows, err := b.ForPage(page, chunkSize).Get(ctx, g)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, row := range rows {
				if !yield(row, nil) {
					return
				}
			}
			if len(rows) < chunkSize {
				return
			}
		}
	}
}

// LazyById answers Concerns\BuildsQueries::lazyById: Lazy walking by key
// instead of by offset, so that a table written to during the walk neither
// repeats a row nor skips one.
func (b *Builder) LazyById(ctx context.Context, g auth.Grant, chunkSize int, column, alias string) iter.Seq2[Record, error] {
	return b.orderedLazyById(ctx, g, chunkSize, column, alias, false)
}

// LazyByIdDesc answers Concerns\BuildsQueries::lazyByIdDesc.
func (b *Builder) LazyByIdDesc(ctx context.Context, g auth.Grant, chunkSize int, column, alias string) iter.Seq2[Record, error] {
	return b.orderedLazyById(ctx, g, chunkSize, column, alias, true)
}

// orderedLazyById answers Concerns\BuildsQueries::orderedLazyById, which is
// protected there.
func (b *Builder) orderedLazyById(ctx context.Context, g auth.Grant, chunkSize int, column, alias string, descending bool) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		if chunkSize < 1 {
			yield(nil, errors.New("query: the chunk size should be at least 1"))
			return
		}
		if column == "" {
			column = b.defaultKeyName()
		}
		if alias == "" {
			alias = column
		}

		var lastId any
		for {
			clone := b.Clone()
			if descending {
				clone.ForPageBeforeId(chunkSize, lastId, column)
			} else {
				clone.ForPageAfterId(chunkSize, lastId, column)
			}

			rows, err := clone.Get(ctx, g)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, row := range rows {
				if !yield(row, nil) {
					return
				}
			}
			if len(rows) < chunkSize {
				return
			}

			lastId = rows[len(rows)-1][alias]
			if lastId == nil {
				yield(nil, fmt.Errorf("query: the lazyById operation was aborted because the [%s] column is not present in the query result", alias))
				return
			}
		}
	}
}

// Cursor answers Builder::cursor.
//
// It is the one read that does not materialise its rows: the connection hands
// them over one at a time, and nothing here holds more than the row being
// yielded. That needs a connection that can stream, which is CursorConnection;
// a connection without it gets an error rather than a select that quietly reads
// the whole table into memory.
func (b *Builder) Cursor(ctx context.Context, g auth.Grant) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		query, err := b.scoped(ctx, g)
		if err != nil {
			yield(nil, err)
			return
		}
		if query.Columns == nil {
			query.Columns = []any{"*"}
		}

		connection, ok := query.Connection.(CursorConnection)
		if !ok {
			yield(nil, errors.New("query: this connection cannot stream a result set; it does not implement query.CursorConnection"))
			return
		}

		rows, err := connection.Cursor(query.ToSQL(), query.GetBindings(), !query.UseWritePDO)
		if err != nil {
			yield(nil, err)
			return
		}
		rows(func(row Record, err error) bool {
			return yield(row, err)
		})
	}
}
