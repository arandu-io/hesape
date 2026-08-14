package broadcasters

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// NullBroadcaster is Illuminate\Broadcasting\Broadcasters\NullBroadcaster:
// every method has an empty body and nothing leaves the process.
//
// It is the driver a connection resolves to when none is configured, so an
// application that never set broadcasting up still runs -- which is what
// BroadcastManager::getConfig arranges by answering ['driver' => 'null'] for a
// missing name.
type NullBroadcaster struct {
	Broadcaster
}

// NewNullBroadcaster builds the driver. The PHP class has no constructor.
func NewNullBroadcaster() *NullBroadcaster { return &NullBroadcaster{} }

// Auth is NullBroadcaster::auth, whose body is a comment.
//
// The zero auth.Grant is the Go shape of the PHP's null: it fails every
// auth.Grant.Check, so a caller that took this answer for an authorization
// reaches nothing. Nobody is authorized by a driver that does not broadcast.
func (n *NullBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	return auth.Grant{}, nil, nil
}

// ValidAuthenticationResponse is NullBroadcaster::validAuthenticationResponse,
// whose body is a comment.
func (n *NullBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	return nil, nil
}

// Broadcast is NullBroadcaster::broadcast, whose body is a comment.
func (n *NullBroadcaster) Broadcast(ctx context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	return nil
}
