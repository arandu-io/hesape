package broadcasting

// Dispatcher is the little of Illuminate\Contracts\Events\Dispatcher that a
// PendingBroadcast uses: it hands the event over, and whatever is listening
// decides what to do with it.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/events so that an application can broadcast
// through anything that dispatches -- and so that this package does not pull
// the outbox and its database in behind it. *events.Dispatcher satisfies it.
type Dispatcher interface {
	// Dispatch is Dispatcher::dispatch: it fires the event and answers whatever
	// the listeners returned.
	Dispatch(event any, payload ...any) []any
}

// BroadcastsVia is what PendingBroadcast::via looks for with
// method_exists($this->event, 'broadcastVia').
//
// Go has no method_exists, so the question is a type assertion and the answer
// is an interface. An event that embeds [InteractsWithBroadcasting] satisfies
// it, which is exactly the set of events the PHP's method_exists says yes to.
type BroadcastsVia interface {
	BroadcastVia(connections ...string) *InteractsWithBroadcasting
}

// ExcludesCurrentUser is what PendingBroadcast::toOthers looks for with
// method_exists($this->event, 'dontBroadcastToCurrentUser'). An event that
// embeds [InteractsWithSockets] satisfies it.
type ExcludesCurrentUser interface {
	DontBroadcastToCurrentUser(socket string) *InteractsWithSockets
}

// PendingBroadcast is Illuminate\Broadcasting\PendingBroadcast: an event on its
// way to the dispatcher, with two things still adjustable.
//
// The PHP dispatches in __destruct, so `broadcast($event)->toOthers();` sends
// when the temporary goes out of scope at the end of the statement. Go has no
// destructor -- this is the first of the three reasons ADR 0044 allows a method
// to be missing -- so the send is [PendingBroadcast.Send] and it is called by
// hand. Nothing else about the type changes, and the chain reads the same:
//
//	manager.Event(e).ToOthers(socket).Send()
type PendingBroadcast struct {
	// events is the PHP's $events. Nil on a FakePendingBroadcast, and Send
	// answers nothing rather than panicking, because the PHP's fake destructor
	// is empty.
	events Dispatcher
	// event is the PHP's $event.
	event any
}

// NewPendingBroadcast is PendingBroadcast::__construct.
func NewPendingBroadcast(events Dispatcher, event any) *PendingBroadcast {
	return &PendingBroadcast{events: events, event: event}
}

// Via is PendingBroadcast::via: broadcast the event using a specific
// broadcaster.
//
// As in the PHP, an event that cannot be told which connection to use -- one
// that does not embed [InteractsWithBroadcasting] -- is left alone rather than
// refused. An empty connection is the PHP's null: the default one.
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

// ToOthers is PendingBroadcast::toOthers: broadcast the event to everyone
// except the current user.
//
// The PHP takes no argument because dontBroadcastToCurrentUser reads the socket
// id off the Broadcast facade. There is no facade and no ambient request here,
// so the socket id is the argument -- see [InteractsWithSockets]. It comes from
// [BroadcastManager.SocketID].
func (p *PendingBroadcast) ToOthers(socket string) *PendingBroadcast {
	if excludes, ok := p.event.(ExcludesCurrentUser); ok {
		excludes.DontBroadcastToCurrentUser(socket)
	}

	return p
}

// Send is PendingBroadcast::__destruct made explicit: it dispatches the event.
//
// It answers what the listeners answered, which is what
// Dispatcher::dispatch returns. Calling it twice dispatches twice -- the PHP
// destructor cannot be called twice, and this is the price of the reason (1)
// substitution being an ordinary method.
func (p *PendingBroadcast) Send() []any {
	if p.events == nil {
		return nil
	}

	return p.events.Dispatch(p.event)
}

// Event exposes the event this broadcast is carrying.
//
// The PHP property is protected and nothing outside the class reads it, but
// nothing outside the class needs to: PHP's destructor makes the value
// disposable. Here the value is held, and a test that wants to know what is
// about to be sent has no other way in.
func (p *PendingBroadcast) Event() any { return p.event }

// FakePendingBroadcast is Illuminate\Broadcasting\FakePendingBroadcast: the
// same three methods, and nothing leaves.
//
// The PHP extends PendingBroadcast and overrides every method with an empty
// body, including the destructor. Go has no virtual dispatch through an
// embedded struct, so the embedding here is for the field set and the
// substitution is by type: a caller holding a *FakePendingBroadcast calls these
// methods, and they are the empty ones.
type FakePendingBroadcast struct {
	PendingBroadcast
}

// NewFakePendingBroadcast is FakePendingBroadcast::__construct, whose body is a
// comment.
func NewFakePendingBroadcast() *FakePendingBroadcast { return &FakePendingBroadcast{} }

// Via is FakePendingBroadcast::via: it changes nothing.
func (f *FakePendingBroadcast) Via(connection string) *FakePendingBroadcast { return f }

// ToOthers is FakePendingBroadcast::toOthers: it changes nothing.
func (f *FakePendingBroadcast) ToOthers(socket string) *FakePendingBroadcast { return f }

// Send is FakePendingBroadcast::__destruct: nothing is dispatched.
func (f *FakePendingBroadcast) Send() []any { return nil }
