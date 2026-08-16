package broadcasters

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// NullBroadcaster has an empty body in every method, and nothing leaves the
// process.
//
// It is the driver a connection resolves to when none is configured, so an
// application that never set broadcasting up still runs.
type NullBroadcaster struct {
	Broadcaster
}

// NewNullBroadcaster builds the driver.
func NewNullBroadcaster() *NullBroadcaster { return &NullBroadcaster{} }

// Auth authorizes nobody: it answers the zero auth.Grant, which fails every
// auth.Grant.Check, so a caller that took this answer for an authorization
// reaches nothing.
func (n *NullBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	return auth.Grant{}, nil, nil
}

// ValidAuthenticationResponse answers nothing, because [NullBroadcaster.Auth]
// authorizes nobody.
func (n *NullBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	return nil, nil
}

// Broadcast drops the event.
func (n *NullBroadcaster) Broadcast(ctx context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	return nil
}
