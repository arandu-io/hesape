package broadcasting

// InteractsWithSockets is Illuminate\Broadcasting\InteractsWithSockets.
//
// The PHP is a trait an event uses. Go has no traits, so it is a struct an
// event embeds, and the two methods are promoted exactly as the trait's are:
//
//	type OrderShipped struct {
//		broadcasting.InteractsWithSockets
//		OrderID string
//	}
//
// The one difference is the argument. The trait's dontBroadcastToCurrentUser
// reads the socket id off the Broadcast facade, which reads it off the ambient
// current request. There is no facade (ADR 0001) and there is no ambient
// request in Go -- a request is a value a handler holds -- so the socket id is
// passed in. [BroadcastManager.SocketID] is where it comes from, and it reads
// the same X-Socket-ID header the facade does.
type InteractsWithSockets struct {
	// Socket is the socket id of the connection that raised the event, and the
	// public $socket of the trait. Empty means the event goes to everyone.
	//
	// It is tagged so that it lands in a broadcast payload under the key the PHP
	// reflection produces -- "socket", not "Socket".
	Socket string `json:"socket"`
}

// DontBroadcastToCurrentUser is InteractsWithSockets::dontBroadcastToCurrentUser:
// it excludes the connection that raised the event from receiving it.
func (i *InteractsWithSockets) DontBroadcastToCurrentUser(socket string) *InteractsWithSockets {
	i.Socket = socket

	return i
}

// BroadcastToEveryone is InteractsWithSockets::broadcastToEveryone.
func (i *InteractsWithSockets) BroadcastToEveryone() *InteractsWithSockets {
	i.Socket = ""

	return i
}
