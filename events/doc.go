// Package events is Illuminate\Events: a dispatcher, its listeners, and an
// outbox underneath.
//
// # The surface is Laravel's
//
// Listen, Dispatch, Until, Subscribe, Push, Flush, Forget, ForgetPushed,
// GetListeners, MakeListener, CreateClassListener, Defer -- the names, the
// arguments and the behaviour are Dispatcher.php's, read from the body and not
// from the signature. A wildcard listener is handed the event name and the
// payload; every other listener is handed the payload spread across its
// parameters; a listener that returns false stops the ones behind it; Until is
// dispatch with $halt set, which is all until() does in the PHP.
//
// The files this package answers to, in the clone at laravel_illuminate/events,
// which is Laravel 13:
//
//	CallQueuedListener.php
//	Dispatcher.php
//	EventServiceProvider.php
//	InvokeQueuedClosure.php
//	NullDispatcher.php
//	QueuedClosure.php
//	functions.php
//
// EventServiceProvider::register is the one method with no equivalent: it binds
// the dispatcher into the container, and there is no container (ADR 0001).
// Wire the dispatcher in bootstrap/app.go, which is where an Arandu project
// wires everything.
//
// # The outbox is the mechanism
//
// A dispatcher on its own loses data in both directions: if the process dies
// between the write and the dispatch, the event never leaves; if a listener
// runs and the transaction rolls back, the rest of the system reacted to
// something that did not happen.
//
// So an event that has to survive the request is stored in the same transaction
// as the write that produced it -- Outbox.Store -- and Relay publishes it
// afterwards, into a Publisher. A *Dispatcher is a Publisher: Publish hands the
// stored row to the listeners registered for its name. That is the one place
// the two halves meet, and it is why an application registers everything with
// Listen and never has to know which half delivered it.
//
// Delivery is at-least-once, which is the price of never losing an event: the
// consumer deduplicates on Stored.ID, and that is why the id is stable and why
// it travels with the event.
//
// # What is not here
//
// Three things in Dispatcher.php have no Go equivalent, and each one is a
// language difference rather than a decision:
//
// addInterfaceListeners walks class_implements($eventName) to find the
// listeners registered against the event's interfaces. Go cannot go from a name
// to a type at run time, and an interface is structural rather than declared,
// so there is nothing to walk.
//
// broadcastEvent pulls a BroadcastFactory out of the container. There is no
// container (ADR 0001), and an event that has to leave the process goes through
// the outbox, which is the one delivery this package owns.
//
// The __call forwarding in NullDispatcher is PHP's, and Go has no method
// missing hook. The methods that mattered are written out.
package events
