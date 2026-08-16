package connections

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// Subscribe listens on a set of channels and calls callback for each message.
//
// It blocks until ctx is cancelled, and that is the whole of its contract: a
// subscriber is a loop, not a registration. Run it in its own goroutine, and
// cancel the context to stop it.
//
// The callback is handed the message and the channel it arrived on, in that
// order.
//
// The channel names are prefixed like keys, so an application only hears its
// own traffic.
func (c *Connection) Subscribe(ctx context.Context, channels []string, callback func(message, channel string)) error {
	return c.CreateSubscription(ctx, channels, callback, "subscribe")
}

// PSubscribe listens on a set of channel patterns and calls callback for each
// message.
//
// It answers Connection::psubscribe(). The patterns are globs -- "orders.*" --
// and the callback is handed the channel the message actually arrived on, not
// the pattern that matched it.
func (c *Connection) PSubscribe(ctx context.Context, channels []string, callback func(message, channel string)) error {
	return c.CreateSubscription(ctx, channels, callback, "psubscribe")
}

// CreateSubscription opens the subscription the other two are named for.
//
// method is "subscribe" or "psubscribe"; anything else is an error rather than
// a silent subscribe,
// because a typo that quietly listens to a literal channel called "orders.*" is
// a subscriber that receives nothing and reports success.
//
// It returns when ctx is cancelled, or when the connection to the server is
// lost -- a subscriber whose socket died must not sit there believing it is
// still listening.
func (c *Connection) CreateSubscription(ctx context.Context, channels []string, callback func(message, channel string), method string) error {
	full := c.keys(channels)

	var pubsub *goredis.PubSub
	switch method {
	case "subscribe":
		pubsub = c.client.Subscribe(ctx, full...)
	case "psubscribe":
		pubsub = c.client.PSubscribe(ctx, full...)
	default:
		return fmt.Errorf("redis: unknown subscription method %q: it is subscribe or psubscribe", method)
	}
	defer func() { _ = pubsub.Close() }()

	messages := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-messages:
			if !ok {
				return nil
			}
			callback(message.Payload, message.Channel)
		}
	}
}
