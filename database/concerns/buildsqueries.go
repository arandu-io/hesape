package concerns

import (
	"context"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// Chunkable answers what Illuminate\Database\Concerns\BuildsQueries asks of the
// query it walks.
//
// The trait is mixed into Query\Builder and Eloquent\Builder in PHP, where it
// reaches straight for $this->offset(), $this->get() and clone $this. Go has no
// traits, and a function that took *query.Builder would only work for that one
// builder -- so the trait's requirements are written out as an interface here,
// which is what "the interface belongs with its consumer" means for a mixin.
//
// T is the row type. In PHP every one of these returns a Collection of stdClass;
// a repository here knows what it selects, so the chunk arrives typed.
//
// Get takes an auth.Grant because it executes. RULE 17 does not bend for a
// read: List, Find, First, Get, Paginate, an export and a report all pass
// through a Policy and filter by auth.Tenant(g). The Grant travels from the
// caller of Chunk down to every page it fetches, so a chunked read is
// authorized once and stays authorized.
type Chunkable[T any] interface {
	// GetOffset answers Builder::getOffset. Nil is the PHP's null.
	GetOffset() *int

	// GetLimit answers Builder::getLimit. Nil is the PHP's null.
	GetLimit() *int

	// Offset answers Builder::offset.
	Offset(value int)

	// Limit answers Builder::limit.
	Limit(value int)

	// ForPage answers Builder::forPage.
	ForPage(page, perPage int)

	// Get answers Builder::get: it runs the query.
	Get(ctx context.Context, g auth.Grant) ([]T, error)

	// EnforceOrderBy answers Builder::enforceOrderBy, which throws when the
	// query has no order. Chunking an unordered result set returns rows twice
	// and skips others, and the database is within its rights.
	EnforceOrderBy() error
}

// KeyChunkable is what ChunkByID adds to Chunkable: paging by a comparison
// against the last id seen rather than by an offset.
//
// It is a separate interface because the id-based methods are the ones a
// builder can only offer when it knows its key, and because Chunk works
// without them.
type KeyChunkable[T any] interface {
	Chunkable[T]

	// Clone answers `clone $this`. The trait clones per page so the where it
	// adds for the last id does not accumulate.
	Clone() KeyChunkable[T]

	// DefaultKeyName answers Builder::defaultKeyName.
	DefaultKeyName() string

	// ForPageAfterID answers Builder::forPageAfterId. The PHP spells the last
	// word Id.
	ForPageAfterID(perPage int, lastID any, column string)

	// ForPageBeforeID answers Builder::forPageBeforeId.
	ForPageBeforeID(perPage int, lastID any, column string)

	// ValueOf answers the data_get($results->last(), $alias) the trait uses to
	// read the id off the last row of a chunk. A row here is a T rather than a
	// stdClass, so the builder that produced it is the only thing that can say
	// which field the alias names.
	ValueOf(row T, alias string) (any, bool)
}

// ErrRecordNotFound answers Illuminate\Database\RecordNotFoundException, which
// FirstOrFail throws when nothing matched.
//
// It is declared here rather than in the database package because that package
// imports this one, and Go refuses the cycle. The database package re-exports
// it, so database.ErrRecordNotFound and concerns.ErrRecordNotFound are one
// value under two names.
var ErrRecordNotFound = errors.New("No record found for the given query.")

// ErrRecordsNotFound answers Illuminate\Database\RecordsNotFoundException,
// which Sole raises when nothing matched.
var ErrRecordsNotFound = errors.New("records not found")

// MultipleRecordsFoundError answers
// Illuminate\Database\MultipleRecordsFoundException: Sole found more than one.
type MultipleRecordsFoundError struct {
	// Count is MultipleRecordsFoundException::$count.
	Count int
}

// NewMultipleRecordsFoundError answers MultipleRecordsFoundException::__construct.
func NewMultipleRecordsFoundError(count int) *MultipleRecordsFoundError {
	return &MultipleRecordsFoundError{Count: count}
}

// Error is the PHP's message, verbatim.
func (e *MultipleRecordsFoundError) Error() string {
	return fmt.Sprintf("%d records were found.", e.Count)
}

// GetCount answers MultipleRecordsFoundException::getCount.
func (e *MultipleRecordsFoundError) GetCount() int { return e.Count }

// Chunk answers BuildsQueries::chunk: it walks the result set a page at a time
// so a large table does not have to fit in memory.
//
// The callback answers false to stop, exactly as the PHP's does, and Chunk
// answers false when it was stopped. The PHP throws on an unordered query;
// this returns the error, because a chunk that silently repeats rows is the
// failure the check exists to prevent.
//
// An existing limit and offset on the query are honoured: the offset shifts the
// first page and the limit bounds the total, which is the arithmetic the PHP
// does with $skip and $remaining.
func Chunk[T any](ctx context.Context, g auth.Grant, query Chunkable[T], count int, callback func(results []T, page int) bool) (bool, error) {
	if err := query.EnforceOrderBy(); err != nil {
		return false, err
	}

	skip := 0
	if o := query.GetOffset(); o != nil {
		skip = *o
	}
	remaining := query.GetLimit()

	page := 1
	for {
		offset := (page-1)*count + skip

		limit := count
		if remaining != nil {
			limit = min(count, *remaining)
		}
		if limit == 0 {
			break
		}

		query.Offset(offset)
		query.Limit(limit)

		results, err := query.Get(ctx, g)
		if err != nil {
			return false, err
		}

		countResults := len(results)
		if countResults == 0 {
			break
		}

		if remaining != nil {
			left := max(*remaining-countResults, 0)
			remaining = &left
		}

		if !callback(results, page) {
			return false, nil
		}

		page++

		if countResults != count {
			break
		}
	}

	return true, nil
}

// ChunkMap answers BuildsQueries::chunkMap: chunk, and collect what the
// callback returns for every row.
//
// The PHP defaults $count to 1000. Go has no default arguments, so the count is
// always given -- pass DefaultChunkSize for the PHP's default.
func ChunkMap[T, R any](ctx context.Context, g auth.Grant, query Chunkable[T], count int, callback func(T) R) ([]R, error) {
	var out []R
	_, err := Chunk(ctx, g, query, count, func(items []T, _ int) bool {
		for _, item := range items {
			out = append(out, callback(item))
		}
		return true
	})
	return out, err
}

// DefaultChunkSize is the 1000 the PHP writes as a default argument on chunkMap,
// each, lazy and lazyById.
const DefaultChunkSize = 1000

// Each answers BuildsQueries::each: chunk, and hand the callback one row at a
// time with its index.
//
// The index is the position within the chunk, which is what the PHP's
// `foreach ($results as $key => $value)` yields on a zero-indexed collection.
// EachByID numbers across chunks instead; that difference is the PHP's, kept.
func Each[T any](ctx context.Context, g auth.Grant, query Chunkable[T], count int, callback func(value T, key int) bool) (bool, error) {
	return Chunk(ctx, g, query, count, func(results []T, _ int) bool {
		for key, value := range results {
			if !callback(value, key) {
				return false
			}
		}
		return true
	})
}

// ChunkByID answers BuildsQueries::chunkById: chunk by comparing ids rather
// than by offset.
//
// It is the one to reach for when the rows being walked are also being written
// to. An offset-based chunk over a table that is having rows inserted into it
// skips rows, because page two of a list that grew by one row starts where page
// one already ended.
//
// column empty takes the query's own key; alias empty takes column.
func ChunkByID[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], count int, callback func(results []T, page int) bool, column, alias string) (bool, error) {
	return OrderedChunkByID(ctx, g, query, count, callback, column, alias, false)
}

// ChunkByIDDesc answers BuildsQueries::chunkByIdDesc.
func ChunkByIDDesc[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], count int, callback func(results []T, page int) bool, column, alias string) (bool, error) {
	return OrderedChunkByID(ctx, g, query, count, callback, column, alias, true)
}

// OrderedChunkByID answers BuildsQueries::orderedChunkById, the body the two
// above share.
//
// The PHP throws RuntimeException when the aliased column is not in the result;
// this returns that error. It is worth the words it costs: a chunk whose id
// column was not selected loops on page one forever otherwise.
func OrderedChunkByID[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], count int, callback func(results []T, page int) bool, column, alias string, descending bool) (bool, error) {
	if column == "" {
		column = query.DefaultKeyName()
	}
	if alias == "" {
		alias = column
	}

	var lastID any
	skip := 0
	if o := query.GetOffset(); o != nil {
		skip = *o
	}
	remaining := query.GetLimit()

	page := 1
	for {
		clone := query.Clone()

		if skip != 0 && page > 1 {
			clone.Offset(0)
		}

		limit := count
		if remaining != nil {
			limit = min(count, *remaining)
		}
		if limit == 0 {
			break
		}

		if descending {
			clone.ForPageBeforeID(limit, lastID, column)
		} else {
			clone.ForPageAfterID(limit, lastID, column)
		}

		results, err := clone.Get(ctx, g)
		if err != nil {
			return false, err
		}

		countResults := len(results)
		if countResults == 0 {
			break
		}

		if remaining != nil {
			left := max(*remaining-countResults, 0)
			remaining = &left
		}

		if !callback(results, page) {
			return false, nil
		}

		value, ok := clone.ValueOf(results[countResults-1], alias)
		if !ok || value == nil {
			return false, fmt.Errorf("The chunkById operation was aborted because the [%s] column is not present in the query result.", alias)
		}
		lastID = value

		page++

		if countResults != count {
			break
		}
	}

	return true, nil
}

// EachByID answers BuildsQueries::eachById: chunk by id, and hand the callback
// one row at a time.
//
// The index runs across chunks -- ((page - 1) * count) + key -- which is the
// PHP's arithmetic, and is why it differs from Each.
func EachByID[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], callback func(value T, key int) bool, count int, column, alias string) (bool, error) {
	return ChunkByID(ctx, g, query, count, func(results []T, page int) bool {
		for key, value := range results {
			if !callback(value, (page-1)*count+key) {
				return false
			}
		}
		return true
	}, column, alias)
}

// Lazy answers BuildsQueries::lazy: an iterator over the whole result set,
// fetched a chunk at a time.
//
// The PHP returns a LazyCollection built on a generator. Go 1.23 has range-over-
// func, so this returns an iter.Seq2-shaped function: the loop reads
//
//	for row, err := range concerns.Lazy(ctx, g, q, 1000) {
//
// and an error ends the iteration after being yielded once, so a caller that
// forgets to check it still stops rather than looping on a broken query.
func Lazy[T any](ctx context.Context, g auth.Grant, query Chunkable[T], chunkSize int) func(yield func(T, error) bool) {
	return func(yield func(T, error) bool) {
		var zero T

		if chunkSize < 1 {
			yield(zero, errors.New("The chunk size should be at least 1"))
			return
		}
		if err := query.EnforceOrderBy(); err != nil {
			yield(zero, err)
			return
		}

		page := 1
		for {
			query.ForPage(page, chunkSize)
			page++

			results, err := query.Get(ctx, g)
			if err != nil {
				yield(zero, err)
				return
			}

			for _, result := range results {
				if !yield(result, nil) {
					return
				}
			}

			if len(results) < chunkSize {
				return
			}
		}
	}
}

// LazyByID answers BuildsQueries::lazyById.
func LazyByID[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], chunkSize int, column, alias string) func(yield func(T, error) bool) {
	return orderedLazyByID(ctx, g, query, chunkSize, column, alias, false)
}

// LazyByIDDesc answers BuildsQueries::lazyByIdDesc.
func LazyByIDDesc[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], chunkSize int, column, alias string) func(yield func(T, error) bool) {
	return orderedLazyByID(ctx, g, query, chunkSize, column, alias, true)
}

// orderedLazyByID answers the protected BuildsQueries::orderedLazyById.
func orderedLazyByID[T any](ctx context.Context, g auth.Grant, query KeyChunkable[T], chunkSize int, column, alias string, descending bool) func(yield func(T, error) bool) {
	return func(yield func(T, error) bool) {
		var zero T

		if chunkSize < 1 {
			yield(zero, errors.New("The chunk size should be at least 1"))
			return
		}
		if column == "" {
			column = query.DefaultKeyName()
		}
		if alias == "" {
			alias = column
		}

		var lastID any
		for {
			clone := query.Clone()

			if descending {
				clone.ForPageBeforeID(chunkSize, lastID, column)
			} else {
				clone.ForPageAfterID(chunkSize, lastID, column)
			}

			results, err := clone.Get(ctx, g)
			if err != nil {
				yield(zero, err)
				return
			}

			for _, result := range results {
				if !yield(result, nil) {
					return
				}
			}

			if len(results) < chunkSize {
				return
			}

			value, ok := clone.ValueOf(results[len(results)-1], alias)
			if !ok || value == nil {
				yield(zero, fmt.Errorf("The lazyById operation was aborted because the [%s] column is not present in the query result.", alias))
				return
			}
			lastID = value
		}
	}
}

// First answers BuildsQueries::first: limit one and take it.
//
// The PHP answers null for an empty result. This answers (zero, false), because
// a zero-valued struct is indistinguishable from a row of zeroes and a caller
// that cannot tell them apart writes a bug that only shows up on real data.
func First[T any](ctx context.Context, g auth.Grant, query Chunkable[T]) (T, bool, error) {
	var zero T

	query.Limit(1)

	results, err := query.Get(ctx, g)
	if err != nil {
		return zero, false, err
	}
	if len(results) == 0 {
		return zero, false, nil
	}
	return results[0], true, nil
}

// FirstOrFail answers BuildsQueries::firstOrFail.
//
// message empty takes the PHP's own sentence. The error wraps
// ErrRecordNotFound, so errors.Is keeps working when a caller adds context to
// it and the exception classifier still answers 404.
func FirstOrFail[T any](ctx context.Context, g auth.Grant, query Chunkable[T], message string) (T, error) {
	var zero T

	result, found, err := First(ctx, g, query)
	if err != nil {
		return zero, err
	}
	if found {
		return result, nil
	}
	if message == "" {
		return zero, ErrRecordNotFound
	}
	return zero, fmt.Errorf("%s: %w", message, ErrRecordNotFound)
}

// Sole answers BuildsQueries::sole: the one row that matched, and an error when
// there was none or more than one.
//
// It fetches two rows, not one, which is the whole point: a query that answers
// "the invoice for this reference" has to be able to say that two of them exist.
func Sole[T any](ctx context.Context, g auth.Grant, query Chunkable[T]) (T, error) {
	var zero T

	query.Limit(2)

	results, err := query.Get(ctx, g)
	if err != nil {
		return zero, err
	}
	switch {
	case len(results) == 0:
		return zero, ErrRecordsNotFound
	case len(results) > 1:
		return zero, NewMultipleRecordsFoundError(len(results))
	}
	return results[0], nil
}

// Tap answers BuildsQueries::tap: hand the query to a callback, and give the
// query back.
func Tap[Q any](query Q, callback func(Q)) Q {
	callback(query)
	return query
}

// Pipe answers BuildsQueries::pipe: hand the query to a callback, and give what
// the callback returned back.
//
// The PHP falls back to $this when the callback returns null. Go has no null to
// fall back from -- a function returning R returns an R -- so the fallback is
// the one thing that could not be carried over, and it is not missed: the PHP
// needs it because pipe and tap share one method there.
func Pipe[Q, R any](query Q, callback func(Q) R) R {
	return callback(query)
}
