package broadcasting

// Dispatcher is the little of an event dispatcher that a [PendingBroadcast]
// uses: it hands the event over, and whatever is listening decides what to do
// with it.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/events so that an application can broadcast
// through anything that dispatches -- and so that this package does not pull
// the outbox and its database in behind it. *events.Dispatcher satisfies it.
type Dispatcher interface {
	// Dispatch fires the event and answers whatever the listeners returned.
	Dispatch(event any, payload ...any) []any
}

// BroadcastsVia is what [PendingBroadcast.Via] looks for: an event that can be
// told which connection to go out on. An event that embeds
// [InteractsWithBroadcasting] satisfies it.
type BroadcastsVia interface {
	BroadcastVia(connections ...string) *InteractsWithBroadcasting
}

// ExcludesCurrentUser is what [PendingBroadcast.ToOthers] looks for: an event
// that can be told which connection not to go out to. An event that embeds
// [InteractsWithSockets] satisfies it.
type ExcludesCurrentUser interface {
	DontBroadcastToCurrentUser(socket string) *InteractsWithSockets
}

// PendingBroadcast is an event on its way to the dispatcher, with two things
// still adjustable.
//
// Nothing sends it on the way out of scope: [PendingBroadcast.Send] is called
// by hand, at the end of the chain.
//
//	manager.Event(e).ToOthers(socket).Send()
type PendingBroadcast struct {
	// events is the dispatcher Send hands the event to. It is nil on a
	// [FakePendingBroadcast], and Send then answers nothing rather than
	// panicking.
	events Dispatcher
	// event is what is being broadcast.
	event any
}

// NewPendingBroadcast builds the broadcast an event is sent through.
func NewPendingBroadcast(events Dispatcher, event any) *PendingBroadcast {
	return &PendingBroadcast{events: events, event: event}
}

// Via broadcasts the event using a specific broadcaster.
//
// An event that cannot be told which connection to use -- one that does not
// embed [InteractsWithBroadcasting] -- is left alone rather than refused. An
// empty connection is the default one.
func (p *PendingBroadcast) Via(connection string) *PendingBroadcast {
	if via, ok := p.event.(BroadcastsVia); ok {
		if connection == "" {
			via.BroadcastVia()
		} else {
			via.BroadcastVia(connection)
		}
	}

	return p
}

// ToOthers broadcasts the event to everyone except the current user.
//
// The socket id is an argument because there is no ambient request to read it
// off -- see [InteractsWithSockets]. It comes from [BroadcastManager.Socket].
func (p *PendingBroadcast) ToOthers(socket string) *PendingBroadcast {
	if excludes, ok := p.event.(ExcludesCurrentUser); ok {
		excludes.DontBroadcastToCurrentUser(socket)
	}

	return p
}

// Send dispatches the event, and answers what the listeners answered.
//
// Calling it twice dispatches twice: it is an ordinary method, and nothing
// marks a broadcast as already sent.
func (p *PendingBroadcast) Send() []any {
	if p.events == nil {
		return nil
	}

	return p.events.Dispatch(p.event)
}

// Event exposes the event this broadcast is carrying, which is the only way to
// see what is about to be sent.
func (p *PendingBroadcast) Event() any { return p.event }

// FakePendingBroadcast is the same three methods, and nothing leaves.
//
// Go has no virtual dispatch through an embedded struct, so the embedding here
// is for the field set and the substitution is by type: a caller holding a
// *FakePendingBroadcast calls these methods, and they are the empty ones.
type FakePendingBroadcast struct {
	PendingBroadcast
}

// NewFakePendingBroadcast builds a broadcast that sends nothing.
func NewFakePendingBroadcast() *FakePendingBroadcast { return &FakePendingBroadcast{} }

// Via changes nothing.
func (f *FakePendingBroadcast) Via(connection string) *FakePendingBroadcast { return f }

// ToOthers changes nothing.
func (f *FakePendingBroadcast) ToOthers(socket string) *FakePendingBroadcast { return f }

// Send dispatches nothing.
func (f *FakePendingBroadcast) Send() []any { return nil }
