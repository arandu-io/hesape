// Package concerns mirrors Illuminate\Database\Concerns.
//
// A PHP trait is textual inclusion: `use ManagesTransactions` copies the bodies
// into the class, and $this inside them is the class. Go has no such thing, so
// each trait here takes one of two shapes, chosen by what the trait actually
// does:
//
//   - a trait that carries state becomes a struct the user embeds, with the
//     calls it makes back into its user written out as an interface --
//     ManagesTransactions, which Connection embeds;
//   - a trait that carries none becomes functions taking what $this was --
//     BuildsQueries, BuildsWhereDateClauses, CompilesJsonPaths,
//     ExplainsQueries and ParsesSearchPath.
//
// The names are the PHP's throughout, with the two mechanical changes ADR 0044
// allows: an error return where the PHP throws, and initialisms in upper case
// (toSql -> ToSQL, chunkById -> ChunkByID, wrapJsonPath -> WrapJSONPath).
//
// # Every read here carries a Grant
//
// Chunk, Each, First, FirstOrFail, Sole, Lazy and Explain all execute, so they
// all take an auth.Grant and hand it to the query on every page they fetch.
// RULE 17 is not a rule about writes: a chunked export that skipped the Policy
// is a leak between tenants with a pleasant name, and a chunk loop is exactly
// where somebody would be tempted to authorize once and then not.
//
// # Two of these are declared here for a reason worth reading
//
// DeadlockError and ErrRecordNotFound belong to Illuminate\Database by
// namespace, and are declared here instead. The database package imports this
// one -- Connection embeds ManagesTransactions -- so declaring them there would
// close an import cycle. The database package re-exports both as aliases, so
// database.DeadlockException and concerns.DeadlockError are one type, and
// errors.Is works across the two names because there is only one.
//
// # The files it answers to
//
// In the clone at laravel_illuminate/database/Concerns:
//
//	BuildsQueries.php
//	BuildsWhereDateClauses.php
//	CompilesJsonPaths.php
//	ExplainsQueries.php
//	ManagesTransactions.php
//	ParsesSearchPath.php
//
// The paginator half of BuildsQueries -- paginateUsingCursor, paginator,
// simplePaginator, cursorPaginator -- is not here: those three build their
// result by asking the container for a class name, which is the pattern ADR
// 0001 and 0002 removed. hesape/pagination builds a page from rows somebody
// already fetched, and that is the one way.
package concerns
