package broadcasters

import (
	"log/slog"

	"github.com/arandu-io/hesape/broadcasting"
)

// The names a connection's Driver field carries.
const (
	// LogDriver names [LogBroadcaster].
	LogDriver = "log"
	// NullDriver names [NullBroadcaster], and is the driver a connection falls
	// back to when none is configured.
	NullDriver = "null"
	// RedisDriver names [RedisBroadcaster].
	RedisDriver = "redis"
)

// CreateLogDriver builds the creator for [LogBroadcaster].
//
// It is a function rather than a method on the manager, and the reason is the
// import graph: BroadcastManager lives in
// github.com/arandu-io/hesape/broadcasting, this package imports that one for
// Channel and BroadcastError, and Go refuses the cycle a method constructing a
// LogBroadcaster would close.
//
// The logger is the argument, and the returned creator is what
// [broadcasting.BroadcastManager.Extend] takes.
func CreateLogDriver(logger *slog.Logger) broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewLogBroadcaster(logger), nil
	}
}

// CreateNullDriver builds the creator for [NullBroadcaster]. See
// [CreateLogDriver] for why it is a function.
func CreateNullDriver() broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewNullBroadcaster(), nil
	}
}

// CreateRedisDriver builds the creator for [RedisBroadcaster]. See
// [CreateLogDriver] for why it is a function.
//
// The connection name and the key prefix are read off
// [broadcasting.ConnectionConfig].
func CreateRedisDriver(redis RedisFactory) broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewRedisBroadcaster(redis, config.Connection, config.Prefix), nil
	}
}

// Register puts the three drivers this ecosystem carries on a manager.
//
// No driver is built in: this package cannot be imported by the one the manager
// is in, so all three are registered the same way a custom driver is, with
// Extend. That is wiring, and wiring belongs in bootstrap/app.go.
//
// A nil logger is slog.Default and a nil factory still registers the redis
// driver -- it fails when it is used, naming itself, rather than at start-up
// for a connection nobody configured.
func Register(m *broadcasting.BroadcastManager, logger *slog.Logger, redis RedisFactory) *broadcasting.BroadcastManager {
	m.Extend(LogDriver, CreateLogDriver(logger))
	m.Extend(NullDriver, CreateNullDriver())
	m.Extend(RedisDriver, CreateRedisDriver(redis))

	return m
}
