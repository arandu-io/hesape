package broadcasting

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// The two paths BroadcastManager::routes and ::userRoutes register.
const (
	// AuthRoute is the '/broadcasting/auth' of routes().
	AuthRoute = "/broadcasting/auth"
	// UserAuthRoute is the '/broadcasting/user-auth' of userRoutes().
	UserAuthRoute = "/broadcasting/user-auth"
)

// SocketIDHeader is the header BroadcastManager::socket reads.
const SocketIDHeader = "X-Socket-ID"

// Config answers config/broadcasting.php.
//
// Illuminate reads it out of the container through $app['config']. There is no
// container here (ADR 0001), so the manager is handed the configuration it
// would otherwise resolve.
type Config struct {
	// Default answers broadcasting.default, which GetDefaultDriver returns.
	Default string
	// Connections answers broadcasting.connections.
	Connections map[string]ConnectionConfig
}

// ConnectionConfig answers one entry of broadcasting.connections.
type ConnectionConfig struct {
	// Driver answers `driver`: "log", "null", "redis", or a name registered
	// with Extend.
	Driver string
	// Connection answers `connection`, the named connection the redis driver
	// publishes through.
	Connection string
	// Prefix answers the database.redis.options.prefix that
	// createRedisDriver passes to the broadcaster.
	Prefix string
	// Options answers `options`, and is what a driver registered with Extend
	// reads its own settings out of.
	Options map[string]any
}

// DriverCreator builds a driver from its configuration. It is the closure
// BroadcastManager::extend registers, and the shape of every create*Driver
// method in the PHP.
type DriverCreator func(config ConnectionConfig) (Broadcaster, error)

// Router is the little of Illuminate's router that routes() and userRoutes()
// use: a pattern and a handler.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/routing so that this package does not depend on
// the router to be usable, and because *net/http.ServeMux satisfies it as it
// stands -- its patterns carry the method, which is the ['get', 'post'] of the
// PHP's match().
type Router interface {
	// Handle registers a handler, e.g. Handle("GET /broadcasting/auth", h).
	Handle(pattern string, handler http.Handler)
}

// Queue is the little of Illuminate\Contracts\Queue\Factory that
// [BroadcastManager.Queue] uses.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/queue so that an application can broadcast
// without a queue behind it, and so that this package does not pull the
// worker, the drivers and their database in.
type Queue interface {
	// PushOn is Queue::pushOn: it puts the job on a named queue of a named
	// connection. Both names may be empty, which is the PHP's null: the
	// default queue of the default connection.
	//
	// The job is *BroadcastEvent, or *UniqueBroadcastEvent when the event asked
	// to be unique.
	PushOn(ctx context.Context, g auth.Grant, connection, queue string, job any) error
}

// UniqueLock is the little of Illuminate\Bus\UniqueLock that
// mustBeUniqueAndCannotAcquireLock uses.
//
// It is an interface rather than github.com/arandu-io/hesape/bus.UniqueLock so
// that a broadcast does not require a cache to be configured, and so this
// package does not depend on the bus. bus.UniqueLock reaches it through the
// two-line adapter that turns the key into its UniqueJob.
type UniqueLock interface {
	// Acquire is UniqueLock::acquire: true when this caller took the lock,
	// false when somebody already holds it.
	Acquire(ctx context.Context, g auth.Grant, key string, expiresAfter time.Duration) (bool, error)
}

// The markers BroadcastManager::queue tests the event against.
//
// A PHP marker interface is empty and the behaviour it marks is found with a
// separate method_exists probe. A Go interface is satisfied by its methods, so
// each marker and its probe are one interface here -- an event that answers the
// method is the event the PHP would have found twice.
type (
	// ShouldBroadcastNow is Illuminate\Contracts\Broadcasting\ShouldBroadcastNow
	// together with the shouldBroadcastNow() probe: the event skips the queue.
	ShouldBroadcastNow interface {
		ShouldBroadcastNow() bool
	}
	// ShouldRescue is Illuminate\Contracts\Broadcasting\ShouldRescue: a failure
	// to publish is swallowed rather than raised, which is what the rescue()
	// helper does around the push in the PHP.
	ShouldRescue interface {
		ShouldRescue() bool
	}
	// HasBroadcastQueue is broadcastQueue() / $broadcastQueue: the queue the
	// event asks to be pushed onto.
	HasBroadcastQueue interface {
		BroadcastQueue() string
	}
)

// BroadcastManager is Illuminate\Broadcasting\BroadcastManager: it resolves
// drivers by name, caches them, and is the entry point for everything that
// leaves this package.
//
// The name is Illuminate's rather than the shorter one Go would pick, because
// BroadcastManager is the name a Laravel developer already knows.
//
// A BroadcastManager is safe for concurrent use.
type BroadcastManager struct {
	mu             sync.Mutex
	config         Config
	events         Dispatcher
	jobs           Queue
	locks          UniqueLock
	drivers        map[string]Broadcaster
	customCreators map[string]DriverCreator
}

// NewBroadcastManager is BroadcastManager::__construct.
//
// The PHP takes the application and pulls the configuration, the event
// dispatcher, the queue and the cache out of it as it needs them. There is no
// container (ADR 0001), so they arrive here. The queue and the lock may be nil:
// an application that only ever broadcasts synchronously never reaches them,
// and one that does is told so by name rather than by a nil dereference.
func NewBroadcastManager(config Config, events Dispatcher, jobs Queue, locks UniqueLock) *BroadcastManager {
	return &BroadcastManager{
		config:         config,
		events:         events,
		jobs:           jobs,
		locks:          locks,
		drivers:        map[string]Broadcaster{},
		customCreators: map[string]DriverCreator{},
	}
}

// Routes is BroadcastManager::routes: it registers the channel authentication
// endpoint.
//
// The PHP takes an array of group attributes -- ['middleware' => ['web']] by
// default -- and exempts the route from PreventRequestForgery. Middleware is
// attached by whoever owns the router here, so the group is the caller's and so
// is the CSRF exemption: this endpoint is reached by the socket client with its
// own credentials, and a CSRF token check on it rejects every legitimate
// subscription.
//
// The routesAreCached() early return has no twin: route caching is a PHP
// bootstrap optimisation, and a Go router is built once at start-up anyway.
func (m *BroadcastManager) Routes(r Router) {
	controller := NewBroadcastController(m)

	r.Handle("GET "+AuthRoute, http.HandlerFunc(controller.Authenticate))
	r.Handle("POST "+AuthRoute, http.HandlerFunc(controller.Authenticate))
}

// UserRoutes is BroadcastManager::userRoutes: it registers the user
// authentication endpoint.
func (m *BroadcastManager) UserRoutes(r Router) {
	controller := NewBroadcastController(m)

	r.Handle("GET "+UserAuthRoute, http.HandlerFunc(controller.AuthenticateUser))
	r.Handle("POST "+UserAuthRoute, http.HandlerFunc(controller.AuthenticateUser))
}

// ChannelRoutes is BroadcastManager::channelRoutes, which the PHP documents as
// an alias of routes().
func (m *BroadcastManager) ChannelRoutes(r Router) { m.Routes(r) }

// Socket is BroadcastManager::socket: the socket id of the connection that
// sent this request, read off the X-Socket-ID header.
//
// The PHP falls back to the ambient current request when given none. Go has no
// ambient request, so the request is the argument, and a nil one answers the
// empty string -- which is what the PHP's early return answers when nothing is
// bound.
func (m *BroadcastManager) Socket(r *http.Request) string {
	if r == nil {
		return ""
	}

	return r.Header.Get(SocketIDHeader)
}

// On is BroadcastManager::on: begin an anonymous broadcast to the given
// channels.
func (m *BroadcastManager) On(channels ...Channel) *AnonymousEvent {
	return NewAnonymousEvent(m.events, channels...)
}

// Private is BroadcastManager::private: begin an anonymous broadcast to a
// private channel.
func (m *BroadcastManager) Private(channel string) *AnonymousEvent {
	return m.On(NewPrivateChannel(channel))
}

// Presence is BroadcastManager::presence: begin an anonymous broadcast to a
// presence channel.
func (m *BroadcastManager) Presence(channel string) *AnonymousEvent {
	return m.On(NewPresenceChannel(channel))
}

// Event is BroadcastManager::event: begin broadcasting an event.
//
// The PHP hands the returned PendingBroadcast to the dispatcher in its
// destructor; here [PendingBroadcast.Send] does it, and the chain reads the
// same otherwise.
func (m *BroadcastManager) Event(event any) *PendingBroadcast {
	return NewPendingBroadcast(m.events, event)
}

// Queue is BroadcastManager::queue: put the event on its way.
//
// An event that asked to go now is handled in this goroutine, through
// [BroadcastEvent.Handle]. Everything else is pushed, and an event that asked to
// be unique takes a lock first and is dropped when somebody already holds it.
// The clone is the PHP's `clone $event`: the copy that travels must not change
// under whoever still holds the original.
//
// ctx and g are the two additions [BroadcastEvent.Handle] documents: I/O, and
// RULE 14.
func (m *BroadcastManager) Queue(ctx context.Context, g auth.Grant, event any) error {
	if now, ok := event.(ShouldBroadcastNow); ok && now.ShouldBroadcastNow() {
		return m.rescue(event, NewBroadcastEvent(cloneEvent(event)).Handle(ctx, g, m))
	}

	var job any = NewBroadcastEvent(cloneEvent(event))

	if _, ok := event.(HasUniqueID); ok {
		unique := NewUniqueBroadcastEvent(cloneEvent(event))

		acquired, err := m.mustBeUniqueAndCanAcquireLock(ctx, g, unique)
		if err != nil {
			return m.rescue(event, err)
		}
		if !acquired {
			return nil
		}

		job = unique
	}

	if m.jobs == nil {
		return m.rescue(event, NewBroadcastError("broadcasting: %s has to be queued and this manager was built with no queue", reflect.TypeOf(event)))
	}

	return m.rescue(event, m.jobs.PushOn(ctx, g, connectionOf(event), queueOf(event), job))
}

// mustBeUniqueAndCanAcquireLock is
// BroadcastManager::mustBeUniqueAndCannotAcquireLock, with the sense of the
// answer turned around: a Go bool named for what it permits reads at the call
// site, and the PHP's double negative does not.
//
// The PHP resolves the cache through uniqueVia() or the container. There is no
// container, so a manager built without a lock refuses the event rather than
// pushing a duplicate it promised not to push.
func (m *BroadcastManager) mustBeUniqueAndCanAcquireLock(ctx context.Context, g auth.Grant, event *UniqueBroadcastEvent) (bool, error) {
	if m.locks == nil {
		return false, NewBroadcastError("broadcasting: %s asked to be unique and this manager was built with no lock", event.DisplayName())
	}

	return m.locks.Acquire(ctx, g, "broadcasting:unique:"+event.DisplayName()+":"+event.UniqueID, event.UniqueFor)
}

// rescue is BroadcastManager::rescue: an event that asked for it has its
// failure swallowed rather than raised.
func (m *BroadcastManager) rescue(event any, err error) error {
	if err == nil {
		return nil
	}
	if r, ok := event.(ShouldRescue); ok && r.ShouldRescue() {
		return nil
	}

	return err
}

// Connection is BroadcastManager::connection: get a driver instance.
func (m *BroadcastManager) Connection(name string) (Broadcaster, error) { return m.Driver(name) }

// Driver is BroadcastManager::driver: get a driver instance, resolving and
// caching it the first time.
func (m *BroadcastManager) Driver(name string) (Broadcaster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "" {
		name = m.config.Default
	}
	if driver, ok := m.drivers[name]; ok {
		return driver, nil
	}

	driver, err := m.resolve(name)
	if err != nil {
		return nil, err
	}
	m.drivers[name] = driver

	return driver, nil
}

// resolve is BroadcastManager::resolve. The caller holds the lock.
//
// The three create*Driver methods of the PHP are not here. They construct
// LogBroadcaster, NullBroadcaster and RedisBroadcaster, which live in
// github.com/arandu-io/hesape/broadcasting/broadcasters -- and that package
// imports this one, for Channel and BroadcastError. PHP namespaces may
// reference each other; Go packages may not, so the creators are
// broadcasters.CreateLogDriver, CreateNullDriver and CreateRedisDriver, and
// broadcasters.Register puts all three on a manager in one line. Every name is
// Illuminate's; only the receiver moved.
func (m *BroadcastManager) resolve(name string) (Broadcaster, error) {
	config, ok := m.getConfig(name)
	if !ok {
		return nil, fmt.Errorf("broadcasting: broadcast connection [%s] is not defined", name)
	}

	creator, ok := m.customCreators[config.Driver]
	if !ok {
		return nil, fmt.Errorf("broadcasting: driver [%s] is not supported", config.Driver)
	}

	driver, err := creator(config)
	if err != nil {
		return nil, WrapBroadcastError(err, "broadcasting: failed to create broadcaster for connection %q with error: %v", name, err)
	}

	return driver, nil
}

// getConfig is BroadcastManager::getConfig: the null driver is the answer for
// the name "null" and for no name at all, and it is the answer without being
// configured.
func (m *BroadcastManager) getConfig(name string) (ConnectionConfig, bool) {
	if name == "" || name == "null" {
		return ConnectionConfig{Driver: "null"}, true
	}

	config, ok := m.config.Connections[name]

	return config, ok
}

// Extend is BroadcastManager::extend: register a custom driver creator.
//
// It is also how the three drivers this ecosystem carries are registered -- see
// [BroadcastManager.resolve] for why they are not methods on this type.
func (m *BroadcastManager) Extend(driver string, creator DriverCreator) *BroadcastManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.customCreators[driver] = creator

	return m
}

// GetDefaultDriver is BroadcastManager::getDefaultDriver.
func (m *BroadcastManager) GetDefaultDriver() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.config.Default
}

// SetDefaultDriver is BroadcastManager::setDefaultDriver.
func (m *BroadcastManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.Default = name
}

// Purge is BroadcastManager::purge: forget one resolved driver, so the next
// call to Driver builds it again. An empty name is the default driver, which is
// the PHP's `$name ??= $this->getDefaultDriver()`.
func (m *BroadcastManager) Purge(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "" {
		name = m.config.Default
	}
	delete(m.drivers, name)
}

// ForgetDrivers is BroadcastManager::forgetDrivers: forget every resolved
// driver.
func (m *BroadcastManager) ForgetDrivers() *BroadcastManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.drivers = map[string]Broadcaster{}

	return m
}

// queueOf is the queue-name half of BroadcastManager::queue: broadcastQueue(),
// then the $broadcastQueue property, then $queue.
//
// The #[Queue] attribute and resolveQueueFromQueueRoute have no twin. An
// attribute is a PHP language feature, and a queue route is the queue package's
// to read -- reason (1) and reason (3) of ADR 0056.
func queueOf(event any) string {
	if q, ok := event.(HasBroadcastQueue); ok {
		return q.BroadcastQueue()
	}
	if name := stringFieldOf(event, "BroadcastQueue"); name != "" {
		return name
	}

	return stringFieldOf(event, "Queue")
}

// connectionOf is the `$event->connection` of BroadcastManager::queue: the
// queue connection the job is pushed onto, which is not the broadcast
// connection it is later published through.
func connectionOf(event any) string { return stringFieldOf(event, "Connection") }

// stringFieldOf reads an exported string field off an event, and is the Go
// shape of PHP's isset($event->name).
func stringFieldOf(event any, name string) string {
	value := reflect.ValueOf(event)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}

	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}

	return field.String()
}
