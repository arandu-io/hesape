package broadcasters

import (
	"log/slog"

	"github.com/arandu-io/hesape/broadcasting"
)

// The names BroadcastManager::resolve builds its create*Driver method name out
// of, and the names a connection's `driver` key carries.
const (
	// LogDriver is the 'log' of createLogDriver.
	LogDriver = "log"
	// NullDriver is the 'null' of createNullDriver, and the driver
	// BroadcastManager::getConfig falls back to.
	NullDriver = "null"
	// RedisDriver is the 'redis' of createRedisDriver.
	RedisDriver = "redis"
)

// CreateLogDriver is BroadcastManager::createLogDriver.
//
// It is a function here rather than a method on the manager, and the reason is
// the import graph: BroadcastManager lives in
// github.com/arandu-io/hesape/broadcasting, this package imports that one for
// Channel and BroadcastError, and Go refuses the cycle that a method
// constructing a LogBroadcaster would close. PHP namespaces allow it, which is
// reason (1) of ADR 0056. Every name is Illuminate's; only the receiver moved.
//
// The PHP resolves the logger out of the container. There is none (ADR 0001),
// so it is the argument, and the returned creator is what
// [broadcasting.BroadcastManager.Extend] takes.
func CreateLogDriver(logger *slog.Logger) broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewLogBroadcaster(logger), nil
	}
}

// CreateNullDriver is BroadcastManager::createNullDriver. See [CreateLogDriver]
// for why it is a function.
func CreateNullDriver() broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewNullBroadcaster(), nil
	}
}

// CreateRedisDriver is BroadcastManager::createRedisDriver. See
// [CreateLogDriver] for why it is a function.
//
// The PHP reads the connection name off the connection's configuration and the
// prefix off database.redis.options.prefix; both are read off
// [broadcasting.ConnectionConfig] here, because there is no config repository
// to reach through.
func CreateRedisDriver(redis RedisFactory) broadcasting.DriverCreator {
	return func(config broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return NewRedisBroadcaster(redis, config.Connection, config.Prefix), nil
	}
}

// Register puts the three drivers this ecosystem carries on a manager.
//
// It answers the `'create'.ucfirst($config['driver']).'Driver'` dispatch of
// BroadcastManager::resolve, which finds the built-in drivers by building a
// method name. Go has no dynamic method dispatch and, more to the point, this
// package cannot be imported by the one the manager is in -- so the built-in
// drivers are registered the same way a custom one is, with Extend. That is
// wiring, and wiring belongs in bootstrap/app.go (ADR 0001).
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
