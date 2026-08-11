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

// NewNullDispatcher creates a new event dispatcher instance that does not fire.
func NewNullDispatcher(dispatcher *Dispatcher) *NullDispatcher {
	return &NullDispatcher{dispatcher: dispatcher}
}

// Dispatch does not fire an event.
func (*NullDispatcher) Dispatch(any, ...any) []any { return nil }

// Push does not register an event and payload to be fired later.
func (*NullDispatcher) Push(string, ...any) {}

// Until does not dispatch an event.
func (*NullDispatcher) Until(any, ...any) any { return nil }

// Listen registers an event listener with the underlying dispatcher.
func (n *NullDispatcher) Listen(events any, listener ...any) {
	n.dispatcher.Listen(events, listener...)
}

// HasListeners reports whether a given event has listeners.
func (n *NullDispatcher) HasListeners(eventName string) bool {
	return n.dispatcher.HasListeners(eventName)
}

// Subscribe registers an event subscriber with the underlying dispatcher.
func (n *NullDispatcher) Subscribe(subscriber Subscriber) { n.dispatcher.Subscribe(subscriber) }

// Flush flushes a set of pushed events on the underlying dispatcher.
func (n *NullDispatcher) Flush(event string) { n.dispatcher.Flush(event) }

// Forget removes a set of listeners from the underlying dispatcher.
func (n *NullDispatcher) Forget(event string) { n.dispatcher.Forget(event) }

// ForgetPushed forgets all of the pushed listeners on the underlying
// dispatcher.
func (n *NullDispatcher) ForgetPushed() { n.dispatcher.ForgetPushed() }

// Publish does not deliver a stored event, and reports no failure: an event
// that was never meant to fire is not an event that failed to.
//
// The PHP forwards every other call to the underlying dispatcher through
// __call, which Go does not have. This is the one method that would otherwise
// have arrived that way, and the relay needs it to be here rather than
// forwarded, because forwarding it would fire what this type exists not to
// fire.
func (*NullDispatcher) Publish(context.Context, Stored) error { return nil }
