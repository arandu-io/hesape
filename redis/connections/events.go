package connections

import (
	"context"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/arandu-io/hesape/redis/events"
	"github.com/arandu-io/hesape/redis/limiters"
)

// Command runs a command against the Redis database.
//
// It answers Connection::command(): it times the call and, when a dispatcher is
// set, fires events.CommandExecuted with the command, its arguments and the
// elapsed milliseconds. Every named command in this package goes through it,
// which is why turning events on shows all of them and not a selection.
//
// The name is the command as Redis spells it -- "get", "setex" -- and the
// arguments follow it. Keys are NOT prefixed here: this is the raw door, and
// the named methods have already called Key on their way in.
func (c *Connection) Command(ctx context.Context, name string, parameters ...any) (any, error) {
	start := time.Now()

	args := make([]any, 0, len(parameters)+1)
	args = append(args, name)
	args = append(args, parameters...)

	result, err := c.client.Do(ctx, args...).Result()

	c.fireCommandExecuted(strings.ToLower(name), parameters, time.Since(start))

	if err == goredis.Nil {
		return nil, err
	}
	return result, err
}

// fireCommandExecuted dispatches the event when somebody is listening.
//
// It rounds to two decimal places the way Laravel does, so the number in a log
// line is the number in the PHP one.
func (c *Connection) fireCommandExecuted(name string, parameters []any, elapsed time.Duration) {
	c.mu.RLock()
	dispatcher := c.events
	c.mu.RUnlock()

	if dispatcher == nil {
		return
	}
	ms := float64(elapsed.Microseconds()) / 1000
	dispatcher.Dispatch(events.Name, events.NewCommandExecuted(name, parameters, ms, c))
}

// timed is what every named command in this package runs through.
//
// It exists so that the event fires for the typed methods too: those go through
// go-redis's own builders rather than through Do, so there is no single place
// the driver would report them from. It is a function and not a method because
// Go methods cannot take type parameters.
func timed[T any](c *Connection, name string, parameters []any, fn func() (T, error)) (T, error) {
	start := time.Now()
	value, err := fn()
	c.fireCommandExecuted(name, parameters, time.Since(start))
	return value, err
}

// Listen registers a Redis command listener with the connection.
//
// It answers Connection::listen(). The callback is handed the
// events.CommandExecuted, and nothing happens at all until a dispatcher is set
// -- see RedisManager.EnableEvents.
func (c *Connection) Listen(callback func(events.CommandExecuted)) {
	c.mu.RLock()
	dispatcher := c.events
	c.mu.RUnlock()

	if dispatcher == nil {
		return
	}
	dispatcher.Listen(events.Name, func(_ string, payload []any) any {
		for _, p := range payload {
			if e, ok := p.(events.CommandExecuted); ok {
				callback(e)
			}
		}
		return nil
	})
}

// GetName is the connection name. It answers Connection::getName().
func (c *Connection) GetName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.name
}

// SetName sets the connection name. RedisManager calls it once, when it hands
// the connection out. It answers Connection::setName().
func (c *Connection) SetName(name string) *Connection {
	c.mu.Lock()
	c.name = name
	c.mu.Unlock()
	return c
}

// GetEventDispatcher is the dispatcher used by the connection, or nil.
//
// It answers Connection::getEventDispatcher().
func (c *Connection) GetEventDispatcher() Dispatcher {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.events
}

// SetEventDispatcher sets the event dispatcher instance on the connection.
//
// It answers Connection::setEventDispatcher(). Until it is called the
// connection fires nothing, which is what makes the per-command event free when
// nobody asked for it.
func (c *Connection) SetEventDispatcher(dispatcher Dispatcher) {
	c.mu.Lock()
	c.events = dispatcher
	c.mu.Unlock()
}

// UnsetEventDispatcher removes the event dispatcher from the connection.
//
// It answers Connection::unsetEventDispatcher().
func (c *Connection) UnsetEventDispatcher() {
	c.mu.Lock()
	c.events = nil
	c.mu.Unlock()
}

// Funnel limits a callback to a maximum number of simultaneous executions.
//
// It answers Connection::funnel(), and returns the builder:
//
//	err := conn.Funnel("export").Limit(3).Then(ctx, run, nil)
func (c *Connection) Funnel(name string) *limiters.ConcurrencyLimiterBuilder {
	return limiters.NewConcurrencyLimiterBuilder(c, name)
}

// Throttle limits a callback to a maximum number of executions over a given
// duration.
//
// It answers Connection::throttle():
//
//	err := conn.Throttle("reports").Allow(10).Every(time.Minute).Then(ctx, run, nil)
func (c *Connection) Throttle(name string) *limiters.DurationLimiterBuilder {
	return limiters.NewDurationLimiterBuilder(c, name)
}

// Disconnect closes the connection. It answers Connection::disconnect(), and is
// the same thing as Close, which is the name the module lifecycle calls.
func (c *Connection) Disconnect() error { return c.Close() }

// The limiters take the connection through an interface of their own, so that
// this package can offer Funnel and Throttle without the two importing each
// other. This is the compile-time check that the interface still fits.
var _ limiters.Connection = (*Connection)(nil)
