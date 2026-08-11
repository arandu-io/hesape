package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// The instance names ConcurrencyManager resolves. They are the strings between
// "create" and "Driver" in the PHP method names, which is how
// MultipleInstanceManager::resolve turns a configured name into a factory call:
// "process" reaches createProcessDriver, "fork" reaches createForkDriver,
// "sync" reaches createSyncDriver.
//
// All three answer with the same driver here. A configuration ported from
// Laravel keeps working and means what it said -- run these at the same time --
// because the goroutine is what fork and proc_open were standing in for. The
// package comment says why there is one instead of three.
const (
	// DriverProcess is the name createProcessDriver answers to, and the one
	// getDefaultInstance falls back to.
	DriverProcess = "process"

	// DriverFork is the name createForkDriver answers to. In PHP it is the fast
	// one and it needs spatie/fork and a console process; here it is the same
	// goroutine as the other two.
	DriverFork = "fork"

	// DriverSync is the name createSyncDriver answers to. In PHP it runs the
	// tasks one after another in the current process, which is how a test suite
	// gets a fixed order; here [Run] already answers in argument order whatever
	// the tasks did, so the fixed order costs nothing and the race detector
	// still sees the real thing.
	DriverSync = "sync"
)

// ErrDriverNotSupported answers the InvalidArgumentException
// MultipleInstanceManager::resolve throws for a driver it has no factory for.
// The message carries the driver name, as the PHP's does.
var ErrDriverNotSupported = errors.New("concurrency: instance driver is not supported")

// ErrDriverNotSpecified answers the RuntimeException
// MultipleInstanceManager::resolve throws when an instance's configuration
// exists but has no driver key.
var ErrDriverNotSpecified = errors.New("concurrency: instance does not specify a driver")

// Config is the part of Illuminate\Contracts\Config\Repository
// ConcurrencyManager reads and writes: concurrency.default and
// concurrency.driver for the name of the default instance,
// concurrency.driver.<name> for one instance's settings.
//
// It is an interface and not a concrete type so that this package does not
// import the configuration package to read two keys out of it. The signatures
// are the ones config.Repository already has, so *config.Repository satisfies
// this without knowing it exists. PHP reads $this->app['config'] out of the
// container, and ADR 0002 rejected the container: the repository is passed in
// instead, which is the same dependency written down rather than resolved.
type Config interface {
	// Get answers Repository::get: the value at a dotted key, or the single
	// optional default when the key is absent.
	Get(key string, def ...any) any

	// Set answers Repository::set: the value at a dotted key.
	Set(key string, val any)
}

// Driver answers Illuminate\Contracts\Concurrency\Driver: the two methods
// ForkDriver, ProcessDriver and SyncDriver each declare, and the two
// ConcurrencyManager forwards to.
//
// It is generic over the task's result where the PHP is over mixed, and the
// type parameter is on the interface rather than on the methods because Go has
// no generic method. A process that runs tasks of more than one result type
// holds more than one [Manager], the way hesape/pipeline holds more than one
// Hub.
//
// Implement it to register a driver of your own with [Manager.Extend].
type Driver[T any] interface {
	// Run answers Driver::run: the tasks run concurrently and the results come
	// back in argument order. See the package-level [Run] for the edges.
	Run(ctx context.Context, tasks ...Task[T]) ([]T, error)

	// Defer answers Driver::defer: the callback that runs the tasks once the
	// current work is finished. See the package-level [Defer].
	Defer(ctx context.Context, tasks ...Task[T]) func() error
}

// goroutineDriver is ForkDriver, ProcessDriver and SyncDriver at once: the two
// methods of [Driver] over the package-level [Run] and [Defer].
//
// It is unexported because there is no Illuminate class to name it after -- the
// three it answers for are three ways to reach the same goroutine, and a fourth
// exported name would be a fourth way to say it. [Manager.Driver] hands it out.
type goroutineDriver[T any] struct{}

// Run answers Driver::run.
func (goroutineDriver[T]) Run(ctx context.Context, tasks ...Task[T]) ([]T, error) {
	return Run(ctx, tasks...)
}

// Defer answers Driver::defer.
func (goroutineDriver[T]) Defer(ctx context.Context, tasks ...Task[T]) func() error {
	return Defer(ctx, tasks...)
}

// Manager answers Illuminate\Concurrency\ConcurrencyManager: it picks a driver
// by name from configuration, keeps the one it built, and forwards run and
// defer to it.
//
// It is the layer the Concurrency facade sits on in PHP, and it is here for the
// two things it does that the package-level [Run] and [Defer] cannot: it reads
// the name of the instance out of configuration, and it lets [Manager.Extend]
// put a different implementation behind that name. Code that needs neither
// calls [Run] directly, which is what this forwards to.
//
// The zero value is a Manager with no configuration -- every key absent, so the
// default instance is [DriverProcess] -- and it is usable. It is safe for
// concurrent use; PHP has no lock because a process serves one request, and a
// long-lived Go binary resolves the driver from every request at once.
type Manager[T any] struct {
	config Config

	mu        sync.Mutex
	instances map[string]Driver[T]
	creators  map[string]func(config map[string]any) Driver[T]
	def       string
}

// NewManager answers "new ConcurrencyManager($app)", with the configuration
// repository in place of the container ADR 0002 rejected.
//
// A nil Config is the empty one: every key is absent, so the manager is the
// [DriverProcess] instance and [Manager.SetDefaultInstance] is the only way to
// move it.
func NewManager[T any](config Config) *Manager[T] {
	return &Manager[T]{config: config}
}

// GetDefaultInstance answers ConcurrencyManager::getDefaultInstance: the value
// of concurrency.default, then of concurrency.driver, then [DriverProcess].
//
// It reads the configuration on every call, as the PHP does, so a key written
// after a driver was resolved moves what this reports; the instance already
// built under the old name stays built, which is what caching it in PHP means
// too. With no configuration it reports what [Manager.SetDefaultInstance] was
// last given, and [DriverProcess] before that.
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

// SetDefaultInstance answers ConcurrencyManager::setDefaultInstance.
//
// It writes both keys the PHP writes, concurrency.default and
// concurrency.driver, and the second one is why this is worth reading twice:
// concurrency.driver also holds the per-instance settings
// [Manager.GetInstanceConfig] reads under concurrency.driver.<name>, so writing
// a name there replaces them. That is the PHP's own behaviour, kept because a
// configuration that survives the port has to fail the same way it did.
func (m *Manager[T]) SetDefaultInstance(name string) {
	m.mu.Lock()
	m.def = name
	m.mu.Unlock()

	if m.config != nil {
		m.config.Set("concurrency.default", name)
		m.config.Set("concurrency.driver", name)
	}
}

// GetInstanceConfig answers ConcurrencyManager::getInstanceConfig: the settings
// at concurrency.driver.<name>, or {"driver": name} when there are none.
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

// Driver answers ConcurrencyManager::driver($name = null): the instance
// registered under the given name, or the default one when no name is given.
//
// At most one name may be given, which is PHP's optional argument, and an empty
// one is no name. The instance is built once and reused, as
// MultipleInstanceManager caches it in $this->instances -- which is also why
// [Manager.Extend] called after an instance was resolved does not replace it.
//
// What is built is decided by the driver key of [Manager.GetInstanceConfig],
// not by the name, exactly as MultipleInstanceManager::resolve does it: an
// instance named "reports" configured with {"driver": "fork"} is the fork
// driver. A configuration with no driver key is [ErrDriverNotSpecified], and a
// driver nothing answers for is [ErrDriverNotSupported]; the PHP throws both.
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
			// PHP hands back the null and fails on the next method call, which
			// is a stack trace inside somebody's job rather than at the line
			// that registered the driver.
			return nil, fmt.Errorf("%w: [%s] was extended with a creator that answered nothing", ErrDriverNotSupported, driver)
		}
	case driver == DriverProcess, driver == DriverFork, driver == DriverSync:
		d = goroutineDriver[T]{}
	default:
		return nil, fmt.Errorf("%w: [%s]", ErrDriverNotSupported, driver)
	}

	if m.instances == nil {
		m.instances = make(map[string]Driver[T], 1)
	}
	m.instances[instance] = d
	return d, nil
}

// Extend answers MultipleInstanceManager::extend: it registers a creator for a
// driver name, which [Manager.Driver] calls in place of the built-in one.
//
// The callback is handed the instance configuration, which is the second of the
// two arguments callCustomCreator passes; the first is the container, and ADR
// 0002 rejected it -- whatever the driver needs is captured by the closure that
// registers it. Registering the same name twice keeps the second callback, as
// assigning the array key twice does, and neither replaces an instance
// [Manager.Driver] has already built.
//
// Extending "process", "fork" or "sync" replaces the built-in driver for that
// name. That is not a second way to run tasks: it is the seam the PHP has for
// the one deployment that needs something else behind the name it already
// configured.
//
// The callback runs while the manager is locked, so it must not call back into
// the manager. PHP recurses there; a Go mutex does not, and a creator that asks
// for another driver would stop the goroutine that asked for this one.
func (m *Manager[T]) Extend(name string, callback func(config map[string]any) Driver[T]) *Manager[T] {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.creators == nil {
		m.creators = make(map[string]func(map[string]any) Driver[T], 1)
	}
	m.creators[name] = callback
	return m
}

// Run answers Concurrency::run, which MultipleInstanceManager::__call forwards
// to the default instance. It is the package-level [Run] once the driver has
// been resolved, and the error from resolving it comes back here.
func (m *Manager[T]) Run(ctx context.Context, tasks ...Task[T]) ([]T, error) {
	driver, err := m.Driver()
	if err != nil {
		return nil, err
	}
	return driver.Run(ctx, tasks...)
}

// Defer answers Concurrency::defer, which MultipleInstanceManager::__call
// forwards to the default instance. It is the package-level [Defer] once the
// driver has been resolved.
//
// The second result is the failure to resolve the driver, which PHP throws at
// this line and not when the callback runs -- a misconfigured driver is a boot
// problem, and finding it out after the response has been sent is finding it
// out in a log nobody reads.
func (m *Manager[T]) Defer(ctx context.Context, tasks ...Task[T]) (func() error, error) {
	driver, err := m.Driver()
	if err != nil {
		return nil, err
	}
	return driver.Defer(ctx, tasks...), nil
}
