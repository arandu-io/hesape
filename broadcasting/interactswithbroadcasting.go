package broadcasting

// InteractsWithBroadcasting is Illuminate\Broadcasting\InteractsWithBroadcasting.
//
// The PHP is a trait an event uses; here it is a struct an event embeds, and
// the two methods are promoted exactly as the trait's are.
//
// The trait's $broadcastConnection starts as [null] -- one connection, the
// default one -- and Arr::wrap turns a single connection into a list. The zero
// value of this struct means the same thing: [BroadcastConnections] answers a
// list of one empty name, and an empty name is what [BroadcastManager.Driver]
// resolves to the default driver.
type InteractsWithBroadcasting struct {
	// broadcastConnection is the trait's protected $broadcastConnection. It is
	// unexported because the trait's is protected, and because
	// BroadcastConnections is the reader the PHP gives it.
	broadcastConnection []string
}

// BroadcastVia is InteractsWithBroadcasting::broadcastVia.
//
// The PHP takes UnitEnum|array|string|null and runs it through enum_value and
// Arr::wrap. Go has no unions and no enum objects, so it is a variadic of
// names: none is the PHP null, which restores the default connection.
func (i *InteractsWithBroadcasting) BroadcastVia(connections ...string) *InteractsWithBroadcasting {
	if len(connections) == 0 {
		i.broadcastConnection = nil

		return i
	}
	i.broadcastConnection = append([]string(nil), connections...)

	return i
}

// BroadcastConnections is InteractsWithBroadcasting::broadcastConnections: the
// connections the event should be broadcast on.
//
// It never answers an empty list, because the PHP's default is [null] rather
// than []: an event that named no connection is broadcast once, on the default
// one.
func (i *InteractsWithBroadcasting) BroadcastConnections() []string {
	if len(i.broadcastConnection) == 0 {
		return []string{""}
	}

	return append([]string(nil), i.broadcastConnection...)
}
