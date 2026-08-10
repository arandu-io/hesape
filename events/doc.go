// Package events is domain events with an outbox.
//
// There is no Publish(). The naive flow loses data in both directions: if the
// process dies between the write and the publish, the event never leaves; if the
// publish happens and the transaction rolls back, the rest of the system reacts
// to something that did not happen.
//
// So an event is stored in the same transaction as the write that produced it,
// and a relay publishes it afterwards. One way to do it (RULE 9), and the one
// that cannot lose an event.
//
// Delivery is at-least-once, which is the price of never losing an event: the
// consumer deduplicates on Stored.ID, and that is why the id is stable and why
// it travels with the event.
//
// # What it answers to
//
// This is the address of Illuminate\Events in this collection. The files in the
// clone at laravel_illuminate/events:
//
//	CallQueuedListener.php
//	Dispatcher.php
//	EventServiceProvider.php
//	InvokeQueuedClosure.php
//	NullDispatcher.php
//	QueuedClosure.php
//	functions.php
//
// The Dispatcher is not a type here. Its two jobs are the two functions a
// listener is registered with -- On, which routes one event name, and Listeners,
// which fans out -- and both produce a Publisher, so the relay stays the single
// thing that delivers. There is no NullDispatcher because a module with no relay
// already stores without publishing, and no CallQueuedListener because a
// listener that wants a queue publishes to one.
package events
