// Package grammars mirrors Illuminate\Database\Query\Grammars: one grammar per
// engine, each one compiling a *query.Builder into the SQL that engine speaks.
//
// The source is the clone at laravel_illuminate/database/Query/Grammars, at
// v12.52.0-54-gd59e7abde. The files it answers to:
//
//	Grammar.php         -> Grammar, in grammar.go
//	MySqlGrammar.php    -> MySQLGrammar, in mysql.go
//	MariaDbGrammar.php  -> MariaDBGrammar, in mariadb.go
//	PostgresGrammar.php -> PostgresGrammar, in postgres.go
//	SQLiteGrammar.php   -> SQLiteGrammar, in sqlite.go
//
// Illuminate\Database\Concerns\CompilesJsonPaths, the trait the grammars mix
// in, is in jsonpath.go.
//
// # Where authorization is, and where it is not
//
// Not here. A grammar turns a builder into a string; it decides how a question
// is spelled, never who may ask it. Authorization lives one layer up, in the
// repository that holds an auth.Grant and filters by auth.Tenant(g) -- on
// reads exactly as on writes, because a read path without a policy is a tenant
// reading another tenant's rows. Nothing in this package takes a Grant, and
// nothing in this package should be reachable except through something that
// does.
//
// One consequence shows up in the code: a where clause the grammar cannot spell
// is never dropped. It compiles to a false clause carrying the reason, so the
// query returns nothing instead of returning rows nobody filtered for. See
// unsupportedClause.
//
// # Placeholders are "?" in all four
//
// Postgres numbers its placeholders and this package still emits "?" for it.
// The numbering happens once, at the connection, in database.Dialect.Rebind,
// which rewrites each "?" to $1, $2 and so on over the finished statement while
// leaving the ones inside string literals and comments alone. Doing it here
// would mean a grammar had to know a fragment's position in a statement it has
// not finished building -- a subquery compiled on its own would start over at
// $1 -- and it would give the project two placeholder conventions where one
// does. See Grammar.Parameter.
//
// # Late binding, which Go does not have
//
// PHP resolves $this->compileLock() at run time, so redefining it in a subclass
// changes what compileSelect emits. Go resolves an embedded method at compile
// time, so the base version would always run and every dialect difference would
// vanish without a single failure. Grammar therefore holds a self reference,
// typed as the unexported dialect interface, and the pipeline calls through it.
// A driver grammar embeds *Grammar and points self at itself, which is what
// `extends Grammar` does for the part that is late binding.
//
// The visibility mapping follows from that: a method a driver grammar overrides
// is exported, because it is the extension point, and PHP marks it protected
// only because protected is as open as PHP gets for a subclass. A helper
// nothing overrides stays unexported.
//
// # The base class is abstract, and the compiler enforces it
//
// Grammar deliberately does not implement CompileInsertOrIgnore or
// CompileUpsert -- the two methods Illuminate declares by throwing, because no
// engine spells them the standard way. Without them *Grammar does not satisfy
// query.Grammar and cannot be handed to a builder at all. The PHP finds that
// out when the statement runs.
//
// # Names
//
// ADR 0044: the name is Illuminate's, with the initial raised and initialisms
// in upper case. MySqlGrammar is MySQLGrammar, MariaDbGrammar is
// MariaDBGrammar, compileInsertGetId is CompileInsertGetID, compileJsonLength
// is CompileJSONLength, toSql is ToSQL. Nothing here is named by invention.
//
// The mechanical changes, each one said again at the method that carries it:
//
//   - (T, error) where the PHP throws and the signature is free to say so:
//     CompileJoinLateral, CompileJSONContains, CompileJSONLength, WhereFullText,
//     SupportsStraightJoins, CompileInsertOrIgnoreUsing,
//     CompileInsertOrIgnoreReturning, SubstituteBindingsIntoRawSQL.
//   - A false clause where the PHP throws inside the compile path, whose
//     signature query.Grammar fixes as returning a string.
//   - The empty string where the PHP returns null, since concatenate drops both.
//   - A values map is walked in sorted key order, because a Go map has none and
//     the statement has to be the same on every run for the bindings to line up.
//     See Grammar.CompileInsert.
//   - CompileUpsert takes the update list as column names only, which is the
//     shape query.Grammar declares; the PHP also accepts a column-to-value map.
//
// # Skipped, and why
//
//   - SqlServerGrammar.php: a driver this ecosystem does not carry. The three
//     connectors are pgx, mysql and sqlite.
//   - Macroable, on Illuminate\Database\Grammar: a PHP language interface --
//     __call adding methods at run time, which Go has no form of.
//   - The compileJoins branch that emits a lateral join: it tests
//     `$join instanceof JoinLateralClause`, and query has no JoinLateralClause
//     yet. CompileJoinLateral itself is here on all four grammars, spelling the
//     clause correctly, and the branch that reaches it belongs with the type.
package grammars
