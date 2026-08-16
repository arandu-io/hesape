package broadcasting

// InteractsWithBroadcasting is embedded by an event that chooses the connection
// it goes out on, and promotes the two methods below onto it.
//
// The zero value is one connection, the default one: [BroadcastConnections]
// answers a list of one empty name, and an empty name is what
// [BroadcastManager.Driver] resolves to the default driver.
type InteractsWithBroadcasting struct {
	// broadcastConnection is the chosen connections. It is unexported because
	// BroadcastConnections is the reader.
	broadcastConnection []string
}

// BroadcastVia names the connections the event goes out on. Passing none
// restores the default connection.
func (i *InteractsWithBroadcasting) BroadcastVia(connections ...string) *InteractsWithBroadcasting {
	if len(connections) == 0 {
		i.broadcastConnection = nil

		return i
	}
	i.broadcastConnection = append([]string(nil), connections...)

	return i
}

// BroadcastConnections is the connections the event should be broadcast on.
//
// It never answers an empty list: an event that named no connection is
// broadcast once, on the default one.
func (i *InteractsWithBroadcasting) BroadcastConnections() []string {
	if len(i.broadcastConnection) == 0 {
		return []string{""}
	}

	return append([]string(nil), i.broadcastConnection...)
}
