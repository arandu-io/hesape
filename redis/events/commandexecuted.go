package events

// Name is the event name listeners register for.
//
// PHP dispatches the class and listens for the class name; Go has no class
// name to pass around, so the dispatcher takes a string and this constant is
// it. Spelling it out here means the listener and the emitter cannot drift.
const Name = "redis.command.executed"

// Named is the part of a connection this event reads.
//
// It exists so that this package does not import connections, which imports
// this one to fire the event.
type Named interface {
	// GetName is the connection name, as RedisManager set it.
	GetName() string
}

// CommandExecuted answers Illuminate\Redis\Events\CommandExecuted.
//
// It is dispatched after every command run through connections.Connection, and
// it is how "which Redis calls did this page make, and how slow were they"
// gets answered without a profiler.
type CommandExecuted struct {
	// Command is the Redis command that was executed, lowercased -- "get",
	// "setex", "zadd".
	Command string

	// Parameters is the array of command parameters, as they were passed. Keys
	// appear here already carrying the application prefix, because that is what
	// went on the wire.
	Parameters []any

	// Time is the number of milliseconds it took to execute the command.
	//
	// It is a float because Laravel's is: sub-millisecond commands are the
	// common case, and rounding them to zero would make the sum of a page's
	// Redis time zero.
	Time float64

	// Connection is the Redis connection instance.
	//
	// It is typed as any rather than as *connections.Connection for the reason
	// Named exists: that package imports this one.
	Connection any

	// ConnectionName is the Redis connection name.
	ConnectionName string
}

// NewCommandExecuted builds the event.
//
// It reads the name off the connection the way the PHP constructor does, so a
// listener never has to ask the connection for it.
func NewCommandExecuted(command string, parameters []any, milliseconds float64, connection Named) CommandExecuted {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return CommandExecuted{
		Command:        command,
		Parameters:     parameters,
		Time:           milliseconds,
		Connection:     connection,
		ConnectionName: name,
	}
}
