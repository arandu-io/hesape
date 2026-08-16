package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// The instance names [Manager.Driver] resolves.
//
// All three resolve to the same driver. The package comment says why there is
// one instead of three.
const (
	// DriverProcess is the name [Manager.CreateProcessDriver] is registered
	// under, and the one [Manager.GetDefaultInstance] falls back to.
	DriverProcess = "process"

	// DriverFork is the name [Manager.CreateForkDriver] is registered under.
	DriverFork = "fork"

	// DriverSync is the name [Manager.CreateSyncDriver] is registered under.
	DriverSync = "sync"
)

// ErrDriverNotSupported is returned for an instance whose driver name nothing
// answers for.
var ErrDriverNotSupported = errors.New("concurrency: instance driver is not supported")

// ErrDriverNotSpecified is returned when an instance's configuration exists but
// has no driver key.
var ErrDriverNotSpecified = errors.New("concurrency: instance does not specify a driver")

// Config is the part of a configuration repository [Manager] reads and writes:
// concurrency.default and concurrency.driver for the name of the default
// instance, concurrency.driver.<name> for one instance's settings.
//
// It is an interface and not a concrete type so that this package does not
// import the configuration package to read two keys out of it. The signatures
// are the ones config.Repository already has, so *config.Repository satisfies
// this without knowing it exists.
type Config interface {
	// Get returns the value at a dotted key, or the single optional default
	// when the key is absent.
	Get(key string, def ...any) any

	// Set writes a value at a dotted key.
	Set(key string, val any)
}

// Driver is what [Manager] resolves a name to and forwards to: a way of
// running a set of tasks concurrently and a way of deferring them.
//
// A process that runs tasks of more than one result type holds more than one
// [Manager], the way hesape/pipeline holds more than one Hub.
//
// Implement it to register a driver of your own with [Manager.Extend].
type Driver[T any] interface {
	// Run runs the tasks concurrently and returns the results in argument
	// order. See the package-level [Run] for the edges.
	Run(ctx context.Context, tasks ...Task[T]) ([]T, error)

	// Defer returns the callback that runs the tasks once the current work is
	// finished. See the package-level [Defer].
	Defer(ctx context.Context, tasks ...Task[T]) func() error
}

// goroutineDriver is the built-in [Driver]: its two methods are the
// package-level [Run] and [Defer].
//
// [Manager.Driver] hands it out for all three built-in names.
type goroutineDriver[T any] struct{}

// Run runs the tasks concurrently and returns the results in argument order.
func (goroutineDriver[T]) Run(ctx context.Context, tasks ...Task[T]) ([]T, error) {
	return Run(ctx, tasks...)
}

// Defer returns the callback that runs the tasks once the current work is
// finished.
func (goroutineDriver[T]) Defer(ctx context.Context, tasks ...Task[T]) func() error {
	return Defer(ctx, tasks...)
}

// Manager picks a driver by name from configuration, keeps the one it built,
// and forwards run and defer to it.
//
// Code that needs neither calls [Run] directly, which is what this forwards
// to.
//
// The zero value is a Manager with no configuration -- every key absent, so
// the default instance is [DriverProcess] -- and it is usable. It is safe for
// concurrent use.
type Manager[T any] struct {
	config Config

	mu        sync.Mutex
	instances map[string]Driver[T]
	creators  map[string]func(config map[string]any) Driver[T]
	def       string
}

// NewManager creates a manager that reads its instances from the given
// configuration repository.
//
// A nil Config is the empty one: every key is absent, so the manager is the
// [DriverProcess] instance and [Manager.SetDefaultInstance] is the only way to
// move it.
func NewManager[T any](config Config) *Manager[T] {
	return &Manager[T]{config: config}
}

// GetDefaultInstance returns the name of the instance used when none is given:
// the value of concurrency.default, then of concurrency.driver, then
// [DriverProcess].
//
// With no configuration it reports what [Manager.SetDefaultInstance] was last
// given, and [DriverProcess] before that.
func (m *Manager[T]) GetDefaultInstance() string {
	if m.config != nil {
		if name, ok := m.config.Get("concurrency.default").(string); ok && name != "" {
			return name
		}
		if name, ok := m.config.Get("concurrency.driver").(string); ok && name != "" {
			return name
		}
	}

	m.mu.Lock()
	def := m.def
	m.mu.Unlock()

	if def != "" {
		return def
	}
	return DriverProcess
}

// SetDefaultInstance sets the name of the instance used when none is given,
// writing it to concurrency.default and concurrency.driver when there is a
// configuration repository to write to.
func (m *Manager[T]) SetDefaultInstance(name string) {
	m.mu.Lock()
	m.def = name
	m.mu.Unlock()

	if m.config != nil {
		m.config.Set("concurrency.default", name)
		m.config.Set("concurrency.driver", name)
	}
}

// GetInstanceConfig returns the settings at concurrency.driver.<name>, or
// {"driver": name} when there are none.
//
// The fallback is what makes "process", "fork" and "sync" resolve with no
// configuration file at all: the name is taken as the driver. It is also what a
// custom creator registered with [Manager.Extend] is handed, so a driver of
// your own reads its own settings from the same place.
func (m *Manager[T]) GetInstanceConfig(name string) map[string]any {
	if m.config != nil {
		if cfg, ok := m.config.Get("concurrency.driver." + name).(map[string]any); ok && cfg != nil {
			return cfg
		}
	}
	return map[string]any{"driver": name}
}

// Driver returns the instance registered under the given name, or the default
// one when no name is given. The instance is built once and kept.
//
// At most one name may be given.
//
// What is built is decided by the driver key of [Manager.GetInstanceConfig],
// not by the name. A configuration with no driver key is
// [ErrDriverNotSpecified], and a driver nothing answers for is
// [ErrDriverNotSupported].
func (m *Manager[T]) Driver(name ...string) (Driver[T], error) {
	instance := ""
	if len(name) > 0 {
		instance = name[0]
	}
	if instance == "" {
		instance = m.GetDefaultInstance()
	}

	// Read before the lock: GetDefaultInstance takes it, and GetInstanceConfig
	// touches only the configuration, which nothing here writes.
	cfg := m.GetInstanceConfig(instance)

	m.mu.Lock()
	defer m.mu.Unlock()

	if d, ok := m.instances[instance]; ok {
		return d, nil
	}

	driver, ok := cfg["driver"].(string)
	if !ok || driver == "" {
		return nil, fmt.Errorf("%w: [%s]", ErrDriverNotSpecified, instance)
	}

	var d Driver[T]
	switch creator := m.creators[driver]; {
	case creator != nil:
		d = creator(cfg)
		if d == nil {
			// Reported here rather than handed back: a nil driver fails on
			// the next method call, which is a stack trace inside somebody's
			// job rather than at the line that registered the driver.
			return nil, fmt.Errorf("%w: [%s] was extended with a creator that answered nothing", ErrDriverNotSupported, driver)
		}
	case driver == DriverProcess:
		d = m.CreateProcessDriver()
	case driver == DriverFork:
		d = m.CreateForkDriver()
	case driver == DriverSync:
		d = m.CreateSyncDriver()
	default:
		return nil, fmt.Errorf("%w: [%s]", ErrDriverNotSupported, driver)
	}

	if m.instances == nil {
		m.instances = make(map[string]Driver[T], 1)
	}
	m.instances[instance] = d
	return d, nil
}

// CreateProcessDriver returns the instance [Manager.Driver] builds for a
// configuration whose driver key is [DriverProcess].
//
// It is the same driver [Manager.CreateForkDriver] and
// [Manager.CreateSyncDriver] return. The package comment says why the three
// are one.
func (m *Manager[T]) CreateProcessDriver() Driver[T] {
	return goroutineDriver[T]{}
}

// CreateForkDriver returns the instance [Manager.Driver] builds for a
// configuration whose driver key is [DriverFork].
//
// It is the same driver [Manager.CreateProcessDriver] returns, and it never
// fails: a goroutine forks nothing, needs no extension, and is available in a
// request handler exactly as it is in a command.
func (m *Manager[T]) CreateForkDriver() Driver[T] {
	return goroutineDriver[T]{}
}

// CreateSyncDriver returns the instance [Manager.Driver] builds for a
// configuration whose driver key is [DriverSync].
//
// It is the same driver the other two return, and it does not run the tasks
// one at a time. [Run] returns results in argument order whatever the tasks
// did, so the ordering a sequential driver would buy is already guaranteed --
// and running them at the same time keeps the race detector looking at the
// code that will run in production.
func (m *Manager[T]) CreateSyncDriver() Driver[T] {
	return goroutineDriver[T]{}
}

// Extend registers a creator for a driver name, which [Manager.Driver] calls
// in place of the built-in one. It returns the manager, so calls chain.
//
// The callback is handed the instance configuration [Manager.GetInstanceConfig]
// returns. Registering the same name twice keeps the second callback, and
// neither replaces an instance [Manager.Driver] has already built.
//
// Extending "process", "fork" or "sync" replaces the built-in driver for that
// name.
//
// The callback runs while the manager is locked, so it must not call back into
// the manager.
func (m *Manager[T]) Extend(name string, callback func(config map[string]any) Driver[T]) *Manager[T] {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.creators == nil {
		m.creators = make(map[string]func(map[string]any) Driver[T], 1)
	}
	m.creators[name] = callback
	return m
}

// Run runs the tasks on the default instance. It is the package-level [Run]
// once the driver has been resolved, and the error from resolving it comes
// back here.
func (m *Manager[T]) Run(ctx context.Context, tasks ...Task[T]) ([]T, error) {
	driver, err := m.Driver()
	if err != nil {
		return nil, err
	}
	return driver.Run(ctx, tasks...)
}

// Defer defers the tasks on the default instance. It is the package-level
// [Defer] once the driver has been resolved.
//
// The second result is the failure to resolve the driver.
func (m *Manager[T]) Defer(ctx context.Context, tasks ...Task[T]) (func() error, error) {
	driver, err := m.Driver()
	if err != nil {
		return nil, err
	}
	return driver.Defer(ctx, tasks...), nil
}
