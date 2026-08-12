package broadcasting

// AnonymousEvent is Illuminate\Broadcasting\AnonymousEvent: a broadcast with no
// event class behind it.
//
// It is what BroadcastManager::on, ::private and ::presence return, and it is
// built by the same three here -- [BroadcastManager.On],
// [BroadcastManager.Private] and [BroadcastManager.Presence] -- because the PHP
// constructor is the only place the channels are set and the dispatcher it
// sends through comes from the manager.
//
//	manager.Private("orders.17").As("OrderShipped").With(map[string]any{"total": 42}).Send()
//
// The PHP's Dispatchable trait is not here: dispatch() and dispatchIf() are
// static methods that construct the event by forwarding their arguments to the
// constructor, which is PHP's late static binding. Go has no static methods and
// no late static binding, so the constructor is the constructor.
type AnonymousEvent struct {
	InteractsWithBroadcasting
	InteractsWithSockets

	// events is what send() reaches through the broadcast() helper in the PHP.
	events Dispatcher
	// channels is the constructor's $channels, after Arr::wrap.
	channels []Channel
	// connection is the protected ?string $connection.
	connection string
	// name is the protected ?string $name.
	name string
	// payload is the protected array $payload.
	payload map[string]any
	// includeCurrentUser is the protected bool $includeCurrentUser, and it
	// starts true.
	includeCurrentUser bool
	// socket is the id toOthers will exclude. The PHP reads it off the facade
	// inside send(); see [InteractsWithSockets] for why it is carried here.
	socket string
	// shouldBroadcastNow is the protected bool $shouldBroadcastNow.
	shouldBroadcastNow bool
}

// NewAnonymousEvent is AnonymousEvent::__construct.
//
// The PHP takes Channel|array|string and runs Arr::wrap over it; the variadic
// is that wrap. The dispatcher is the one the broadcast() helper would have
// resolved out of the container (ADR 0001).
func NewAnonymousEvent(events Dispatcher, channels ...Channel) *AnonymousEvent {
	return &AnonymousEvent{
		events:             events,
		channels:           append([]Channel(nil), channels...),
		includeCurrentUser: true,
	}
}

// Via is AnonymousEvent::via: the connection the event should be broadcast on.
//
// It is not [InteractsWithBroadcasting.BroadcastVia]. The PHP class declares
// both -- via() sets its own $connection, which send() hands to
// PendingBroadcast::via, which then calls broadcastVia. Keeping the two apart
// is what makes toOthers() and via() composable in either order.
func (e *AnonymousEvent) Via(connection string) *AnonymousEvent {
	e.connection = connection

	return e
}

// As is AnonymousEvent::as: the name the event should be broadcast as.
func (e *AnonymousEvent) As(name string) *AnonymousEvent {
	e.name = name

	return e
}

// With is AnonymousEvent::with: the payload the event should be broadcast with.
//
// The PHP takes Arrayable|array. Go has no union, so it takes any and answers
// the same two shapes: an [Arrayable] is flattened with ToArray, and a
// map[string]any has each of its values flattened the same way, which is the
// Collection::map in the PHP. Anything else is ignored, as an array of the
// wrong shape would be.
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

// ToOthers is AnonymousEvent::toOthers: broadcast to everyone except the
// current user.
//
// The PHP takes no argument and sets a flag; send() then calls
// PendingBroadcast::toOthers, which reaches the facade for the socket id. There
// is no facade here, so the id is the argument and is kept until send() -- see
// [InteractsWithSockets].
func (e *AnonymousEvent) ToOthers(socket string) *AnonymousEvent {
	e.includeCurrentUser = false
	e.socket = socket

	return e
}

// SendNow is AnonymousEvent::sendNow: broadcast the event synchronously.
//
// It sets the flag [AnonymousEvent.ShouldBroadcastNow] answers and sends;
// [BroadcastManager.Queue] is what reads the flag and skips the queue.
func (e *AnonymousEvent) SendNow() []any {
	e.shouldBroadcastNow = true

	return e.Send()
}

// Send is AnonymousEvent::send: broadcast the event.
//
// The PHP body is broadcast($this)->via($this->connection), then toOthers() if
// the current user is excluded. The broadcast() helper is a container lookup
// for the manager; here the dispatcher it would have found is already held.
func (e *AnonymousEvent) Send() []any {
	broadcast := NewPendingBroadcast(e.events, e).Via(e.connection)

	if !e.includeCurrentUser {
		broadcast.ToOthers(e.socket)
	}

	return broadcast.Send()
}

// BroadcastAs is AnonymousEvent::broadcastAs.
//
// The fallback is class_basename($this), which for this class is the literal
// below -- Go has no run-time class name to read, and reflection would answer
// the same string.
func (e *AnonymousEvent) BroadcastAs() string {
	if e.name != "" {
		return e.name
	}

	return "AnonymousEvent"
}

// BroadcastWith is AnonymousEvent::broadcastWith.
func (e *AnonymousEvent) BroadcastWith() map[string]any {
	if e.payload == nil {
		return map[string]any{}
	}

	return e.payload
}

// BroadcastOn is AnonymousEvent::broadcastOn.
func (e *AnonymousEvent) BroadcastOn() []Channel { return e.channels }

// ShouldBroadcastNow is AnonymousEvent::shouldBroadcastNow.
func (e *AnonymousEvent) ShouldBroadcastNow() bool { return e.shouldBroadcastNow }
