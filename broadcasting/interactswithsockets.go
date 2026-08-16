package broadcasting

// InteractsWithSockets is embedded by an event that can exclude the connection
// that raised it, and promotes the two methods below onto it:
//
//	type OrderShipped struct {
//		broadcasting.InteractsWithSockets
//		OrderID string
//	}
//
// The socket id is passed in rather than read off an ambient request, because a
// request in Go is a value a handler holds. [BroadcastManager.Socket] is where
// it comes from, off the X-Socket-ID header.
type InteractsWithSockets struct {
	// Socket is the socket id of the connection that raised the event. Empty
	// means the event goes to everyone.
	//
	// It is tagged so that it lands in a broadcast payload under "socket"
	// rather than "Socket".
	Socket string `json:"socket"`
}

// DontBroadcastToCurrentUser excludes the connection that raised the event from
// receiving it.
func (i *InteractsWithSockets) DontBroadcastToCurrentUser(socket string) *InteractsWithSockets {
	i.Socket = socket

	return i
}

// BroadcastToEveryone clears the exclusion, so the event reaches every
// connection.
func (i *InteractsWithSockets) BroadcastToEveryone() *InteractsWithSockets {
	i.Socket = ""

	return i
}
