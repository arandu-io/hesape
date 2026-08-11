package queue

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arandu-io/hesape/queue/events"
)

// QueueManager holds the configured connections and hands them out by name.
//
// It answers Illuminate\Queue\QueueManager, minus the half of it that is the
// container: there is no lazy resolution from a config array, because the
// application constructs its queues in bootstrap/app.go and passes them here
// (ADR 0001). What is left is the part anybody actually calls -- `aru work
// --connection=redis` has to turn a string into a queue, the default connection
// has to have a name, and a queue has to be pausable while an incident is open.
//
//	m := queue.NewQueueManager().
//		Extend("database", queue.NewDatabaseQueue(db)).
//		Extend("sync", queue.NewSyncQueue(w))
type QueueManager struct {
	mu          sync.RWMutex
	def         string
	connections map[string]Queue
	connectors  map[string]func() (Queue, error)
	routes      *QueueRoutes
	cache       Cache
	events      Dispatcher
}

// NewQueueManager returns an empty manager.
func NewQueueManager() *QueueManager {
	return &QueueManager{
		connections: map[string]Queue{},
		connectors:  map[string]func() (Queue, error){},
		routes:      NewQueueRoutes(),
	}
}

// SetCache gives the manager somewhere to keep the pause flags and the restart
// signal, and returns it so the call chains.
//
// Without one, [QueueManager.Pause] and [QueueManager.Restart] have nowhere to
// write and say so, rather than silently doing nothing -- a queue somebody
// believes is paused and is not is worse than an error.
func (m *QueueManager) SetCache(c Cache) *QueueManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = c
	return m
}

// SetEvents gives the manager somewhere to send QueuePaused and QueueResumed,
// and returns it so the call chains.
func (m *QueueManager) SetEvents(d Dispatcher) *QueueManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = d
	return m
}

// Extend registers a connection under a name.
//
// It answers extend(), which in PHP registers a connector -- a closure that
// builds the queue when it is first asked for. Here it takes the queue itself,
// because there is no config array to build it from and lazily resolving
// something the application already constructed is a delay with no payoff.
// [QueueManager.AddConnector] is the lazy form, for a driver whose connection
// is expensive to open.
//
// The first one registered becomes the default, so an application with one
// queue never has to say which it means.
func (m *QueueManager) Extend(name string, q Queue) *QueueManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.def == "" {
		m.def = name
	}
	if named, says := q.(interface{ SetConnectionName(string) }); says {
		named.SetConnectionName(name)
	}
	m.connections[name] = q
	return m
}

// AddConnector registers how to build a connection the first time it is asked
// for.
//
// It answers addConnector(). It is the one place lazy resolution earns its
// keep: a RESP connection opens a socket, and a binary that runs `aru migrate`
// should not open one on the way past.
//
// The first one registered becomes the default, exactly as with
// [QueueManager.Extend].
func (m *QueueManager) AddConnector(name string, connect func() (Queue, error)) *QueueManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.def == "" {
		m.def = name
	}
	m.connectors[name] = connect
	return m
}

// GetDefaultDriver is the name [QueueManager.Connection] uses when asked for "".
//
// It answers getDefaultDriver().
func (m *QueueManager) GetDefaultDriver() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.def
}

// SetDefaultDriver names the connection [QueueManager.Connection] uses when
// asked for "". It answers setDefaultDriver().
func (m *QueueManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = name
}

// GetName is the connection name to use, resolving "" to the default.
//
// It answers getName(). It is what a command prints, so a log line says
// "database" rather than nothing.
func (m *QueueManager) GetName(name string) string {
	if name != "" {
		return name
	}
	return m.GetDefaultDriver()
}

// Connected reports whether a connection has already been built.
//
// It answers connected(). It is false for a name registered with
// [QueueManager.AddConnector] and never asked for, and true once it has been.
func (m *QueueManager) Connected(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, built := m.connections[m.nameLocked(name)]
	return built
}

// Connection returns a registered queue. An empty name means the default.
//
// It answers connection(), including the caching: a connector runs once and its
// queue is kept.
func (m *QueueManager) Connection(name string) (Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = m.nameLocked(name)

	if q, built := m.connections[name]; built {
		return q, nil
	}
	connect, registered := m.connectors[name]
	if !registered {
		return nil, fmt.Errorf("%w: %q. Register it with Extend in bootstrap/app.go", ErrNoConnection, name)
	}
	q, err := connect()
	if err != nil {
		return nil, fmt.Errorf("queue: opening the %s connection: %w", name, err)
	}
	if named, says := q.(interface{ SetConnectionName(string) }); says {
		named.SetConnectionName(name)
	}
	m.connections[name] = q
	return q, nil
}

// nameLocked resolves "" to the default. The caller holds the lock.
func (m *QueueManager) nameLocked(name string) string {
	if name == "" {
		return m.def
	}
	return name
}

// Connections lists the registered names, for `aru queue:monitor` to print.
//
// Sorted, so two runs of a command print the same order -- a map's is random,
// and a listing that reshuffles is a listing nobody can diff.
func (m *QueueManager) Connections() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]bool{}
	for name := range m.connections {
		seen[name] = true
	}
	for name := range m.connectors {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Route sends a job name to a connection and a queue.
//
// It answers route(). See [QueueRoutes] for what it buys.
func (m *QueueManager) Route(name, queue, connection string) {
	m.mu.RLock()
	routes := m.routes
	m.mu.RUnlock()
	routes.Set(name, queue, connection)
}

// GetRoutes is the routing table, for a dispatcher to consult.
//
// It has no PHP counterpart because in Laravel the table is reached through a
// trait on half a dozen classes; here it is one object with one owner.
func (m *QueueManager) GetRoutes() *QueueRoutes {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routes
}

// pausedKey is where a pause is recorded. It answers Laravel's
// illuminate:queue:paused:{connection}:{queue}.
func pausedKey(connection, queue string) string {
	return "hesape:queue:paused:" + connection + ":" + queue
}

// ErrNoCache is returned when the manager is asked to do something that needs
// somewhere to write and has nowhere.
//
// It is an error rather than a silent no-op, because "the queue is paused" is a
// thing an operator believes after the command returns, and believing it
// wrongly is how work keeps going out during an incident.
var ErrNoCache = fmt.Errorf("queue: no cache is wired, so there is nowhere to record this. Call SetCache in bootstrap/app.go")

// Pause stops workers taking new jobs off a queue, until it is resumed.
//
// It answers pause(). Jobs already running finish; jobs already queued stay
// queued. It is the switch to reach for when a downstream system is failing and
// retrying into it is making things worse.
func (m *QueueManager) Pause(ctx context.Context, connection, queue string) error {
	return m.pauseFor(ctx, connection, queue, 0)
}

// PauseFor stops workers taking new jobs off a queue for a while, after which
// it resumes on its own.
//
// It answers pauseFor(). It is the safer half of [QueueManager.Pause]: a pause
// with a deadline cannot be the thing somebody forgot to undo.
func (m *QueueManager) PauseFor(ctx context.Context, connection, queue string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("queue: PauseFor needs a positive duration. Use Pause for a pause with no deadline")
	}
	return m.pauseFor(ctx, connection, queue, ttl)
}

func (m *QueueManager) pauseFor(ctx context.Context, connection, queue string, ttl time.Duration) error {
	m.mu.RLock()
	store, dispatcher := m.cache, m.events
	m.mu.RUnlock()
	if store == nil {
		return ErrNoCache
	}

	connection = m.GetName(connection)
	// A pause with no deadline is a very long one rather than a forever: every
	// store here takes a ttl, and a year is past any incident and short of the
	// forever that outlives the person who set it.
	keep := ttl
	if keep <= 0 {
		keep = 365 * 24 * time.Hour
	}
	if err := store.Put(ctx, pausedKey(connection, queue), []byte("1"), keep); err != nil {
		return fmt.Errorf("queue: pausing %s on %s: %w", queue, connection, err)
	}
	if dispatcher != nil {
		dispatcher.Dispatch(events.QueuePaused{ConnectionName: connection, Queue: queue, TTL: ttl})
	}
	return nil
}

// Resume lets workers take jobs off a paused queue again. It answers resume().
func (m *QueueManager) Resume(ctx context.Context, connection, queue string) error {
	m.mu.RLock()
	store, dispatcher := m.cache, m.events
	m.mu.RUnlock()
	if store == nil {
		return ErrNoCache
	}

	connection = m.GetName(connection)
	if err := store.Forget(ctx, pausedKey(connection, queue)); err != nil {
		return fmt.Errorf("queue: resuming %s on %s: %w", queue, connection, err)
	}
	if dispatcher != nil {
		dispatcher.Dispatch(events.QueueResumed{ConnectionName: connection, Queue: queue})
	}
	return nil
}

// IsPaused reports whether a queue is paused. It answers isPaused().
//
// A manager with no cache answers false: nothing can have paused a queue when
// there is nowhere to record a pause.
func (m *QueueManager) IsPaused(ctx context.Context, connection, queue string) (bool, error) {
	m.mu.RLock()
	store := m.cache
	m.mu.RUnlock()
	if store == nil {
		return false, nil
	}
	if _, err := store.Get(ctx, pausedKey(m.GetName(connection), queue)); err != nil {
		return false, nil
	}
	return true, nil
}

// Restart asks every running worker to stop after the job it is holding.
//
// It answers `queue:restart`, which in Laravel is a command rather than a
// method on the manager -- the command writes the timestamp the worker polls,
// and it is written here so the command is three lines and the key is named
// once.
//
// It is how a deploy replaces the workers: the new binary starts, the old ones
// notice the timestamp moved and exit cleanly, and whatever supervises them
// starts the new image. RULE 16's sibling -- nothing about it runs at boot.
func (m *QueueManager) Restart(ctx context.Context) error {
	m.mu.RLock()
	store := m.cache
	m.mu.RUnlock()
	if store == nil {
		return ErrNoCache
	}
	// A year, for the same reason a pause is a year: the signal has to outlive
	// the deploy, and a worker started next week compares against whatever is
	// there and stops once.
	if err := store.Put(ctx, restartKey, []byte(nowStamp()), 365*24*time.Hour); err != nil {
		return fmt.Errorf("queue: signalling a restart: %w", err)
	}
	return nil
}

// WithoutInterruptionPolling stops workers reading the cache to find out
// whether they should pause or restart.
//
// It answers withoutInterruptionPolling(). Every worker asking a shared cache
// once a second is a load somebody eventually notices, and an application whose
// deploy stops the processes itself does not need the signal.
//
// It is process-wide, exactly as the PHP statics are: a worker built after this
// is called does not poll either.
func (m *QueueManager) WithoutInterruptionPolling() {
	interruptionPolling.Store(false)
}

// interruptionPolling is Worker::$restartable and Worker::$pausable, which are
// two statics in PHP set by one method and never independently. One flag says
// the same thing without letting them disagree.
var interruptionPolling atomic.Bool

func init() { interruptionPolling.Store(true) }

// Before registers a listener for the event before a job runs. It answers
// before().
//
// The seven registration methods on the PHP manager are sugar over
// $events->listen(SomeEvent::class, $callback), and they are here for the same
// reason they are there: `Queue::failing(...)` in bootstrap/app.go says what it
// does, and the listen call with a class name does not.
func (m *QueueManager) Before(listener any) { m.listen(events.JobProcessing{}, listener) }

// After registers a listener for the event after a job ran. It answers after().
func (m *QueueManager) After(listener any) { m.listen(events.JobProcessed{}, listener) }

// ExceptionOccurred registers a listener for a job that returned an error. It
// answers exceptionOccurred().
func (m *QueueManager) ExceptionOccurred(listener any) {
	m.listen(events.JobExceptionOccurred{}, listener)
}

// Failing registers a listener for a job that gave up. It answers failing().
func (m *QueueManager) Failing(listener any) { m.listen(events.JobFailed{}, listener) }

// Looping registers a listener for each pass of a worker's loop. It answers
// looping().
func (m *QueueManager) Looping(listener any) { m.listen(events.Looping{}, listener) }

// Starting registers a listener for a worker starting. It answers starting().
func (m *QueueManager) Starting(listener any) { m.listen(events.WorkerStarting{}, listener) }

// Stopping registers a listener for a worker stopping. It answers stopping().
func (m *QueueManager) Stopping(listener any) { m.listen(events.WorkerStopping{}, listener) }

// listen registers a listener, when there is a dispatcher to register it with.
func (m *QueueManager) listen(event any, listener any) {
	m.mu.RLock()
	dispatcher := m.events
	m.mu.RUnlock()
	if registrar, can := dispatcher.(interface{ Listen(any, ...any) }); can {
		registrar.Listen(event, listener)
	}
}
