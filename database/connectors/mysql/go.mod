module github.com/arandu-io/hesape/database/connectors/mysql

go 1.26

require (
	github.com/arandu-io/hesape v0.0.0
	github.com/go-sql-driver/mysql v1.9.3
)

require filippo.io/edwards25519 v1.1.0 // indirect

replace github.com/arandu-io/hesape => ../../..
