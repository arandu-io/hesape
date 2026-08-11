package connectors

import (
	"context"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

// Connector opens one queue connection.
//
// It answers Illuminate\Queue\Connectors\ConnectorInterface. In PHP a connector
// takes an array of config and returns a queue, because the container resolves
// drivers by name at runtime and the config is what it has to go on. Here the
// config is a typed struct the application filled in, so the connector is what
// is left: the thing that knows how to open the connection, and can be handed
// to queue.QueueManager.AddConnector so it is not opened until it is needed.
//
//	m.AddConnector("database", connectors.Database{DB: db}.Connect)
//
// A driver whose connection is free -- sync, null, deferred -- does not need
// one. It is registered with Extend, and these exist so the two are wired the
// same way when a person prefers that.
type Connector interface {
	// Connect opens the connection and returns the queue over it.
	Connect(ctx context.Context) (queue.Queue, error)
}

// Database opens a queue over the application's own database.
//
// It answers Illuminate\Queue\Connectors\DatabaseConnector. The config it takes
// is a field rather than an array, which is the whole difference: a typo in a
// config key is a compile error here and a null there.
type Database struct {
	// DB is the connection the queue writes to. It is the application's own,
	// which is what makes a job pushed inside database.Transaction part of that
	// transaction.
	DB *database.DB
}

var _ Connector = Database{}

// Connect returns the queue. It answers connect().
func (c Database) Connect(context.Context) (queue.Queue, error) {
	return queue.NewDatabaseQueue(c.DB), nil
}

// Sync opens a queue that runs each job at push.
//
// It answers Illuminate\Queue\Connectors\SyncConnector.
type Sync struct {
	// Handlers is the registry a job name is resolved against. It is the
	// worker's, so a name is registered once.
	Handlers queue.Handlers
}

var _ Connector = Sync{}

// Connect returns the queue. It answers connect().
func (c Sync) Connect(context.Context) (queue.Queue, error) {
	return queue.NewSyncQueue(c.Handlers), nil
}

// Null opens a queue that keeps nothing.
//
// It answers Illuminate\Queue\Connectors\NullConnector.
type Null struct{}

var _ Connector = Null{}

// Connect returns the queue. It answers connect().
func (Null) Connect(context.Context) (queue.Queue, error) { return queue.NullQueue{}, nil }

// Deferred opens a queue that runs each job after the response.
//
// It answers Illuminate\Queue\Connectors\DeferredConnector.
type Deferred struct {
	// Handlers is the registry a job name is resolved against.
	Handlers queue.Handlers
}

var _ Connector = Deferred{}

// Connect returns the queue. It answers connect().
func (c Deferred) Connect(context.Context) (queue.Queue, error) {
	return queue.NewDeferredQueue(c.Handlers), nil
}

// Background opens a queue that runs each job in another process.
//
// It answers Illuminate\Queue\Connectors\BackgroundConnector.
type Background struct {
	// Run starts the job in another process and returns once it has started.
	// See queue.NewBackgroundQueue for what it must and must not do.
	Run func(ctx context.Context, j jobs.Job) error
}

var _ Connector = Background{}

// Connect returns the queue. It answers connect().
func (c Background) Connect(context.Context) (queue.Queue, error) {
	return queue.NewBackgroundQueue(c.Run), nil
}

// Failover opens a queue that writes to the first connection that accepts a
// job.
//
// It answers Illuminate\Queue\Connectors\FailoverConnector.
type Failover struct {
	// Manager resolves the names below.
	Manager *queue.QueueManager
	// Connections are the connections to try, in order. The first is the one
	// that is read from -- see queue.FailoverQueue for why.
	Connections []string
	// Events is where QueueFailedOver goes, and nil means nowhere.
	Events queue.Dispatcher
}

var _ Connector = Failover{}

// Connect returns the queue. It answers connect().
func (c Failover) Connect(context.Context) (queue.Queue, error) {
	q := queue.NewFailoverQueue(c.Manager, c.Connections...)
	if c.Events != nil {
		q.SetEvents(c.Events)
	}
	return q, nil
}
