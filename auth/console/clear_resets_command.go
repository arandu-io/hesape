package console

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/console"
)

// BrokerFactory hands out password brokers by name. An application can have
// more than one -- a broker per kind of account, each with its own token store
// and its own expiry -- and the command's optional name argument is what picks
// between them.
//
// Only the one method the command calls is declared, and it is declared here
// rather than imported, so the command compiles and is tested without the
// package that builds brokers.
type BrokerFactory interface {
	// Broker is the broker of that name. The empty name means the default one.
	Broker(name string) (Broker, error)
}

// Broker is one password broker: the thing that mints a reset token, gets it
// delivered, and redeems it when the person comes back with it. This command
// resets nobody's password -- it needs the broker only to reach the store those
// tokens sit in.
type Broker interface {
	// GetRepository is where that broker keeps its tokens.
	GetRepository() TokenRepository
}

// TokenRepository is where a broker keeps its reset tokens -- a database table
// or a cache, depending on how the broker was built. This command needs one
// thing of it: throw away the records whose lifetime has passed.
type TokenRepository interface {
	// DeleteExpired sweeps those records. It takes a context because it is a
	// DELETE against a table that can be large, and a command somebody stopped
	// with ctrl-C must not leave one running.
	DeleteExpired(ctx context.Context) error
}

// ErrNoBrokerFactory is returned when the command was built without one.
//
// It is a failure and not a silent success, because the whole command is the
// sweep: a scheduled task that reports success while deleting nothing is a table
// that grows for a year before anybody looks.
var ErrNoBrokerFactory = errors.New("auth: auth:clear-resets has no password broker factory")

// The name, signature and description of the command.
const (
	// Name is what the command is called on the command line.
	Name = "auth:clear-resets"
	// Signature is that name with its one optional argument.
	Signature = "auth:clear-resets {name? : The name of the password broker}"
	// Description is the line the listing shows, lowercased to match the rest of
	// what this console prints.
	Description = "flush expired password reset tokens"
)

// ClearResetsCommand deletes the password reset tokens whose lifetime has
// passed.
//
// Run it from the scheduler; it is safe to run twice, and it takes no lock,
// because deleting an already deleted row is nothing.
type ClearResetsCommand struct {
	brokers BrokerFactory
}

// NewClearResetsCommand returns the command over a password broker factory.
func NewClearResetsCommand(brokers BrokerFactory) *ClearResetsCommand {
	return &ClearResetsCommand{brokers: brokers}
}

// Command is the value the console registry takes.
//
// Nothing is found by scanning: hesape/console holds a slice of values, so that
// the listing and the compiler read the same registry -- see console.Command.
func (c *ClearResetsCommand) Command() console.Command {
	return console.Command{
		Signature:   Signature,
		Description: Description,
		Run:         c.Handle,
	}
}

// Handle executes the console command: it finds the broker the name argument
// picks and sweeps its expired tokens.
//
// The IO is the second parameter because a command holds no state of its own:
// the arguments arrive with the run.
//
// A failure is returned rather than printed, and the console's own exit path
// turns it into a non-zero status.
func (c *ClearResetsCommand) Handle(ctx context.Context, o *console.IO) error {
	if c.brokers == nil {
		return ErrNoBrokerFactory
	}

	broker, err := c.brokers.Broker(o.Argument("name").String())
	if err != nil {
		return err
	}
	if err := broker.GetRepository().DeleteExpired(ctx); err != nil {
		return err
	}

	o.OutputComponents().Info("Expired reset tokens cleared successfully.")
	return nil
}
