package events

import "context"

// NullDispatcher registers listeners and never fires them.
//
// Everything that changes the registry is forwarded to the real dispatcher, and
// everything that fires an event does nothing. That asymmetry is the whole
// point: a subsystem wired through one of these still has its listeners set up,
// so turning it back on is swapping the dispatcher rather than re-registering
// anything.
type NullDispatcher struct {
	dispatcher *Dispatcher
}

// NewNullDispatcher is NullDispatcher::__construct: it creates a new event
// dispatcher instance that does not fire.
func NewNullDispatcher(dispatcher *Dispatcher) *NullDispatcher {
	return &NullDispatcher{dispatcher: dispatcher}
}

// Dispatch is NullDispatcher::dispatch: it does not fire an event.
func (*NullDispatcher) Dispatch(any, ...any) []any { return nil }

// Push is NullDispatcher::push: it does not register an event and payload to be
// fired later.
func (*NullDispatcher) Push(string, ...any) {}

// Until is NullDispatcher::until: it does not dispatch an event.
func (*NullDispatcher) Until(any, ...any) any { return nil }

// Listen is NullDispatcher::listen: it registers an event listener with the
// underlying dispatcher.
func (n *NullDispatcher) Listen(events any, listener ...any) {
	n.dispatcher.Listen(events, listener...)
}

// HasListeners is NullDispatcher::hasListeners: it reports whether a given event
// has listeners.
func (n *NullDispatcher) HasListeners(eventName string) bool {
	return n.dispatcher.HasListeners(eventName)
}

// Subscribe is NullDispatcher::subscribe: it registers an event subscriber with
// the underlying dispatcher.
func (n *NullDispatcher) Subscribe(subscriber Subscriber) { n.dispatcher.Subscribe(subscriber) }

// Flush is NullDispatcher::flush: it flushes a set of pushed events on the
// underlying dispatcher.
func (n *NullDispatcher) Flush(event string) { n.dispatcher.Flush(event) }

// Forget is NullDispatcher::forget: it removes a set of listeners from the
// underlying dispatcher.
func (n *NullDispatcher) Forget(event string) { n.dispatcher.Forget(event) }

// ForgetPushed is NullDispatcher::forgetPushed: it forgets all of the pushed
// listeners on the underlying dispatcher.
func (n *NullDispatcher) ForgetPushed() { n.dispatcher.ForgetPushed() }

// Publish has no Illuminate counterpart: it is the outbox side of the
// dispatcher, and it does not deliver a stored event, and reports no failure --
// an event that was never meant to fire is not an event that failed to.
//
// The nearest thing in the PHP is NullDispatcher::__call, which forwards every
// other call to the underlying dispatcher and which Go does not have. This is
// the one method that would otherwise have arrived that way, and the relay needs
// it to be here rather than forwarded, because forwarding it would fire what
// this type exists not to fire.
func (*NullDispatcher) Publish(context.Context, Stored) error { return nil }
