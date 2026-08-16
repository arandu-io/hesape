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

// The two paths [BroadcastManager.Routes] and [BroadcastManager.UserRoutes]
// register.
const (
	// AuthRoute authorizes a subscription to one channel.
	AuthRoute = "/broadcasting/auth"
	// UserAuthRoute authenticates the connection itself.
	UserAuthRoute = "/broadcasting/user-auth"
)

// SocketIDHeader is the header [BroadcastManager.Socket] reads the socket id of
// the calling connection off.
const SocketIDHeader = "X-Socket-ID"

// Config is the broadcasting configuration a manager is built with.
type Config struct {
	// Default is the connection used when no name is given, and what
	// [BroadcastManager.GetDefaultDriver] answers.
	Default string
	// Connections is every configured connection, by name.
	Connections map[string]ConnectionConfig
}

// ConnectionConfig is one configured broadcast connection.
type ConnectionConfig struct {
	// Driver is "log", "null", "redis", or a name registered with
	// [BroadcastManager.Extend].
	Driver string
	// Connection is the named connection the redis driver publishes through.
	Connection string
	// Prefix is the key prefix the redis driver puts in front of every channel
	// it publishes on.
	Prefix string
	// Options is what a driver registered with [BroadcastManager.Extend] reads
	// its own settings out of.
	Options map[string]any
}

// DriverCreator builds a driver from its configuration. It is what
// [BroadcastManager.Extend] registers.
type DriverCreator func(config ConnectionConfig) (Broadcaster, error)

// Router is the little of a router that [BroadcastManager.Routes] and
// [BroadcastManager.UserRoutes] use: a pattern and a handler.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/routing so that this package does not depend on
// the router to be usable, and because *net/http.ServeMux satisfies it as it
// stands -- its patterns carry the method as well as the path.
type Router interface {
	// Handle registers a handler, e.g. Handle("GET /broadcasting/auth", h).
	Handle(pattern string, handler http.Handler)
}

// Queue is the little of a queue factory that [BroadcastManager.Queue] uses.
//
// It is declared here rather than imported from
// github.com/arandu-io/hesape/queue so that an application can broadcast
// without a queue behind it, and so that this package does not pull the
// worker, the drivers and their database in.
type Queue interface {
	// PushOn puts the job on a named queue of a named connection. Both names
	// may be empty: the default queue of the default connection.
	//
	// The job is *BroadcastEvent, or *UniqueBroadcastEvent when the event asked
	// to be unique.
	PushOn(ctx context.Context, g auth.Grant, connection, queue string, job any) error
}

// UniqueLock is the little of a lock that [BroadcastManager.Queue] uses to keep
// a unique event from being pushed twice.
//
// It is an interface rather than github.com/arandu-io/hesape/bus.UniqueLock so
// that a broadcast does not require a cache to be configured, and so this
// package does not depend on the bus. bus.UniqueLock reaches it through the
// two-line adapter that turns the key into its UniqueJob.
type UniqueLock interface {
	// Acquire is true when this caller took the lock, false when somebody
	// already holds it.
	Acquire(ctx context.Context, g auth.Grant, key string, expiresAfter time.Duration) (bool, error)
}

// The optional interfaces [BroadcastManager.Queue] tests the event against. An
// empty interface would be satisfied by every value, so each marker carries the
// method that answers it.
type (
	// ShouldBroadcastNow returning true makes the event skip the queue: it is
	// published in the goroutine that raised it.
	ShouldBroadcastNow interface {
		ShouldBroadcastNow() bool
	}
	// ShouldRescue returning true swallows a failure to publish rather than
	// returning it.
	ShouldRescue interface {
		ShouldRescue() bool
	}
	// HasBroadcastQueue is the queue the event asks to be pushed onto.
	HasBroadcastQueue interface {
		BroadcastQueue() string
	}
)

// BroadcastManager resolves drivers by name, caches them, and is the entry
// point for everything that leaves this package.
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

// NewBroadcastManager builds a manager over everything it needs: the
// configuration, the dispatcher an anonymous broadcast is sent through, the
// queue and the lock.
//
// The queue and the lock may be nil: an application that only ever broadcasts
// synchronously never reaches them, and one that does is told so by name rather
// than by a nil dereference.
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

// Routes registers the channel authentication endpoint.
//
// Middleware is attached by whoever owns the router, and so is the CSRF
// exemption this endpoint needs: it is reached by the socket client with its
// own credentials, and a CSRF token check on it rejects every legitimate
// subscription.
func (m *BroadcastManager) Routes(r Router) {
	controller := NewBroadcastController(m)

	r.Handle("GET "+AuthRoute, http.HandlerFunc(controller.Authenticate))
	r.Handle("POST "+AuthRoute, http.HandlerFunc(controller.Authenticate))
}

// UserRoutes registers the user authentication endpoint.
func (m *BroadcastManager) UserRoutes(r Router) {
	controller := NewBroadcastController(m)

	r.Handle("GET "+UserAuthRoute, http.HandlerFunc(controller.AuthenticateUser))
	r.Handle("POST "+UserAuthRoute, http.HandlerFunc(controller.AuthenticateUser))
}

// ChannelRoutes is an alias of [BroadcastManager.Routes].
func (m *BroadcastManager) ChannelRoutes(r Router) { m.Routes(r) }

// Socket is the socket id of the connection that sent this request, read off
// the X-Socket-ID header.
//
// The request is an argument because Go has no ambient one, and a nil request
// answers the empty string.
func (m *BroadcastManager) Socket(r *http.Request) string {
	if r == nil {
		return ""
	}

	return r.Header.Get(SocketIDHeader)
}

// On begins an anonymous broadcast to the given channels.
func (m *BroadcastManager) On(channels ...Channel) *AnonymousEvent {
	return NewAnonymousEvent(m.events, channels...)
}

// Private begins an anonymous broadcast to a private channel.
func (m *BroadcastManager) Private(channel string) *AnonymousEvent {
	return m.On(NewPrivateChannel(channel))
}

// Presence begins an anonymous broadcast to a presence channel.
func (m *BroadcastManager) Presence(channel string) *AnonymousEvent {
	return m.On(NewPresenceChannel(channel))
}

// Event begins broadcasting an event.
//
// The returned broadcast reaches the dispatcher through
// [PendingBroadcast.Send], which has to be called: nothing sends it on the way
// out of scope.
func (m *BroadcastManager) Event(event any) *PendingBroadcast {
	return NewPendingBroadcast(m.events, event)
}

// Queue puts the event on its way.
//
// An event that asked to go now is handled in this goroutine, through
// [BroadcastEvent.Handle]. Everything else is pushed, and an event that asked to
// be unique takes a lock first and is dropped when somebody already holds it.
// The event is cloned either way: the copy that travels must not change under
// whoever still holds the original.
//
// ctx and g are what [BroadcastEvent.Handle] documents: the I/O, and the Grant
// the tenant comes off.
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

// mustBeUniqueAndCanAcquireLock takes the lock a unique event is pushed under.
// It is named for what it permits, because a bool named for what it forbids
// reads as a double negative at the call site.
//
// A manager built without a lock refuses the event rather than pushing a
// duplicate it promised not to push.
func (m *BroadcastManager) mustBeUniqueAndCanAcquireLock(ctx context.Context, g auth.Grant, event *UniqueBroadcastEvent) (bool, error) {
	if m.locks == nil {
		return false, NewBroadcastError("broadcasting: %s asked to be unique and this manager was built with no lock", event.DisplayName())
	}

	return m.locks.Acquire(ctx, g, "broadcasting:unique:"+event.DisplayName()+":"+event.UniqueID, event.UniqueFor)
}

// rescue swallows the failure of an event that asked for it, and returns every
// other one.
func (m *BroadcastManager) rescue(event any, err error) error {
	if err == nil {
		return nil
	}
	if r, ok := event.(ShouldRescue); ok && r.ShouldRescue() {
		return nil
	}

	return err
}

// Connection is [BroadcastManager.Driver] under the name the [Factory] contract
// gives it.
func (m *BroadcastManager) Connection(name string) (Broadcaster, error) { return m.Driver(name) }

// Driver returns a driver instance, resolving and caching it the first time. An
// empty name is the default driver.
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

// resolve builds the driver a connection names. The caller holds the lock.
//
// No driver is built in: the three this ecosystem carries live in
// github.com/arandu-io/hesape/broadcasting/broadcasters, and that package
// imports this one for Channel and BroadcastError, so a constructor here would
// close an import cycle. broadcasters.Register puts all three on a manager in
// one line, through [BroadcastManager.Extend].
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

// getConfig reads a connection's configuration. The null driver is the answer
// for the name "null" and for no name at all, and it is the answer without
// being configured.
func (m *BroadcastManager) getConfig(name string) (ConnectionConfig, bool) {
	if name == "" || name == "null" {
		return ConnectionConfig{Driver: "null"}, true
	}

	config, ok := m.config.Connections[name]

	return config, ok
}

// Extend registers a driver creator under a name.
//
// It is also how the three drivers this ecosystem carries are registered -- see
// [BroadcastManager.resolve] for why they are not methods on this type.
func (m *BroadcastManager) Extend(driver string, creator DriverCreator) *BroadcastManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.customCreators[driver] = creator

	return m
}

// GetDefaultDriver is the connection name used when none is given.
func (m *BroadcastManager) GetDefaultDriver() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.config.Default
}

// SetDefaultDriver sets the connection name used when none is given.
func (m *BroadcastManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.Default = name
}

// Purge forgets one resolved driver, so the next call to Driver builds it
// again. An empty name is the default driver.
func (m *BroadcastManager) Purge(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name == "" {
		name = m.config.Default
	}
	delete(m.drivers, name)
}

// ForgetDrivers forgets every resolved driver.
func (m *BroadcastManager) ForgetDrivers() *BroadcastManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.drivers = map[string]Broadcaster{}

	return m
}

// queueOf is the queue an event asks to be pushed onto: its BroadcastQueue
// method, then a BroadcastQueue field, then a Queue field.
func queueOf(event any) string {
	if q, ok := event.(HasBroadcastQueue); ok {
		return q.BroadcastQueue()
	}
	if name := stringFieldOf(event, "BroadcastQueue"); name != "" {
		return name
	}

	return stringFieldOf(event, "Queue")
}

// connectionOf is the queue connection the job is pushed onto, which is not the
// broadcast connection it is later published through.
func connectionOf(event any) string { return stringFieldOf(event, "Connection") }

// stringFieldOf reads an exported string field off an event, and answers the
// empty string when there is none.
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
