// Package events names the three things that happen to a batch.
//
// They are names and not types, for the reason the auth events are: an event
// crosses processes and versions of the binary through the outbox, so what
// travels is a name and a JSON body, and a Go struct on the publishing side
// would be a struct the subscribing side has to have compiled in.
//
// A name is a contract. Change one and the listeners stop hearing, silently,
// which is why they are constants here rather than string literals at the four
// places that would otherwise spell them.
//
// The files it answers to, in the clone at
// laravel_illuminate/bus/Events:
//
//	BatchCanceled.php
//	BatchDispatched.php
//	BatchFinished.php
package events

// The three names. Dotted and lowercase, like every other event in the
// collection.
//
// Cancelled is spelled with two Ls. Laravel's file is BatchCanceled with one,
// which is the American spelling of a word Laravel spells the British way
// everywhere else; the collection picks one spelling and this is it.
const (
	// BatchDispatched is published when a batch's jobs have been queued.
	BatchDispatched = "batch.dispatched"
	// BatchCancelled is published when a batch is cancelled.
	BatchCancelled = "batch.cancelled"
	// BatchFinished is published when a batch stops, successfully or not.
	BatchFinished = "batch.finished"
)
