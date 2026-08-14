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
//
// # There is no Event::fake, because there is nothing global to swap
//
// Event::fake, Event::fakeExcept, Event::fakeFor and Event::fakeExceptFor are
// reason 2, all four -- the ADR 0044 reason for a method that only serves the
// container, a facade or a service provider. Each of them is Facade::swap: it
// puts an EventFake where the 'events' binding was, so that every event() in
// the application lands in the fake without anything else changing. There is
// no container (ADR 0001) and no facade (ADR 0002), so there is no binding to
// put it in -- and a package-level dispatcher a test could swap would be
// shared mutable state, which is the one thing ADR 0045 names as worse than
// the container itself: two tests calling t.Parallel would fight over it.
//
// What a test writes instead is the dispatcher it builds for itself, which is
// already faked by having none of the application's listeners on it:
//
//	d := events.NewDispatcher()
//
//	var shipped []OrderShipped
//	d.Listen(func(e OrderShipped) { shipped = append(shipped, e) })
//
//	placeOrder(ctx, g, d)
//
//	if len(shipped) != 1 {
//		t.Fatalf("the order shipped %d times, want 1", len(shipped))
//	}
//
// Event::fakeExcept is the same dispatcher with the listeners you do want on
// it, which is the argument you leave in rather than the one you take out.
// Event::fakeFor and Event::fakeExceptFor are the scope of the variable: d
// stops existing when the function that built it returns, and that is what the
// PHP's callable argument is buying -- a fake that does not outlive the block
// it was wanted in. Here nothing outlives its block, so there is no second
// form of it.
package events
