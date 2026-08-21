module github.com/arandu-io/hesape/queue/connectors/redis

go 1.26

// Its own module, so a project that queues over its own database does not carry
// a RESP client in its go.sum, its build and its vulnerability surface. In Go
// there is no optional dependency, and this is the only shape that keeps the
// collection's own module down to golang.org/x/crypto.
require (
	github.com/arandu-io/hesape v0.11.0
	github.com/redis/go-redis/v9 v9.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
)
