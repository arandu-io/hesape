module github.com/arandu-io/hesape/database/connectors/pgx

go 1.26

// pgx v5.10.0 or later: earlier versions carry GO-2026-5004, a SQL injection
// reachable from any repository List. govulncheck found it here on its first
// run, in a project that did not even need Postgres.
require (
	github.com/arandu-io/hesape v0.0.0
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)

replace github.com/arandu-io/hesape => ../../..
