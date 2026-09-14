// Package webhook delivers immutable, signed HTTP callbacks through the
// database queue.
//
// Dispatch records one delivery per event and endpoint in the same transaction
// as its queue job. Workers claim that record with a fencing token before
// issuing a request, so concurrent and stale deliveries cannot settle each
// other's attempt. Publisher connects the events outbox to this path without a
// second outbox.
package webhook
