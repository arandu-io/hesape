package broadcasting

// AnonymousEvent is a broadcast with no event type behind it.
//
// It is built by [BroadcastManager.On], [BroadcastManager.Private] and
// [BroadcastManager.Presence], because the constructor is the only place the
// channels are set and the dispatcher it sends through comes from the manager.
//
//	manager.Private("orders.17").As("OrderShipped").With(map[string]any{"total": 42}).Send()
type AnonymousEvent struct {
	InteractsWithBroadcasting
	InteractsWithSockets

	// events is the dispatcher Send hands the broadcast to.
	events Dispatcher
	// channels is what the event goes out on.
	channels []Channel
	// connection is the broadcast connection, empty for the default one.
	connection string
	// name is what the event goes out as, empty for "AnonymousEvent".
	name string
	// payload is what the event goes out with.
	payload map[string]any
	// includeCurrentUser starts true, and ToOthers is what clears it.
	includeCurrentUser bool
	// socket is the id ToOthers will exclude; see [InteractsWithSockets] for
	// why it is carried here.
	socket string
	// shouldBroadcastNow is set by SendNow and read by
	// [BroadcastManager.Queue].
	shouldBroadcastNow bool
}

// NewAnonymousEvent builds an anonymous broadcast over the channels it goes out
// on. The dispatcher is the one [AnonymousEvent.Send] hands the broadcast to.
func NewAnonymousEvent(events Dispatcher, channels ...Channel) *AnonymousEvent {
	return &AnonymousEvent{
		events:             events,
		channels:           append([]Channel(nil), channels...),
		includeCurrentUser: true,
	}
}

// Via is the connection the event should be broadcast on.
//
// It is not [InteractsWithBroadcasting.BroadcastVia]: this one only records the
// name, and Send is what hands it on. Keeping the two apart is what makes
// ToOthers and Via composable in either order.
func (e *AnonymousEvent) Via(connection string) *AnonymousEvent {
	e.connection = connection

	return e
}

// As is the name the event should be broadcast as.
func (e *AnonymousEvent) As(name string) *AnonymousEvent {
	e.name = name

	return e
}

// With is the payload the event should be broadcast with.
//
// Go has no union type, so it takes any and reads two shapes: an [Arrayable] is
// flattened with ToArray, and a map[string]any has each of its values flattened
// the same way. Anything else is ignored.
func (e *AnonymousEvent) With(payload any) *AnonymousEvent {
	switch p := payload.(type) {
	case Arrayable:
		e.payload = p.ToArray()
	case map[string]any:
		flattened := make(map[string]any, len(p))
		for key, value := range p {
			flattened[key] = formatProperty(value)
		}
		e.payload = flattened
	}

	return e
}

// ToOthers broadcasts to everyone except the current user.
//
// The socket id is the argument and is kept until Send, because there is no
// ambient request to read it off -- see [InteractsWithSockets].
func (e *AnonymousEvent) ToOthers(socket string) *AnonymousEvent {
	e.includeCurrentUser = false
	e.socket = socket

	return e
}

// SendNow broadcasts the event synchronously.
//
// It sets the flag [AnonymousEvent.ShouldBroadcastNow] answers and sends;
// [BroadcastManager.Queue] is what reads the flag and skips the queue.
func (e *AnonymousEvent) SendNow() []any {
	e.shouldBroadcastNow = true

	return e.Send()
}

// Send broadcasts the event: it builds the [PendingBroadcast], applies the
// connection and the socket exclusion, and hands it to the dispatcher.
func (e *AnonymousEvent) Send() []any {
	broadcast := NewPendingBroadcast(e.events, e).Via(e.connection)

	if !e.includeCurrentUser {
		broadcast.ToOthers(e.socket)
	}

	return broadcast.Send()
}

// BroadcastAs is the name given to [AnonymousEvent.As], or "AnonymousEvent"
// when none was.
func (e *AnonymousEvent) BroadcastAs() string {
	if e.name != "" {
		return e.name
	}

	return "AnonymousEvent"
}

// BroadcastWith is the payload given to [AnonymousEvent.With], or an empty
// map.
func (e *AnonymousEvent) BroadcastWith() map[string]any {
	if e.payload == nil {
		return map[string]any{}
	}

	return e.payload
}

// BroadcastOn is the channels the event goes out on.
func (e *AnonymousEvent) BroadcastOn() []Channel { return e.channels }

// ShouldBroadcastNow is true when the event was sent with
// [AnonymousEvent.SendNow].
func (e *AnonymousEvent) ShouldBroadcastNow() bool { return e.shouldBroadcastNow }
