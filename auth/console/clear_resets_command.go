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
//
// Answers the part of Illuminate\Contracts\Auth\PasswordBrokerFactory this
// command uses.
type BrokerFactory interface {
	// Broker is PasswordBrokerFactory::broker. The empty name means the default
	// broker, which is what PHP's null argument means.
	Broker(name string) (Broker, error)
}

// Broker is one password broker: the thing that mints a reset token, gets it
// delivered, and redeems it when the person comes back with it. This command
// resets nobody's password -- it needs the broker only to reach the store those
// tokens sit in.
//
// Answers the part of Illuminate\Auth\Passwords\PasswordBroker this command
// uses.
type Broker interface {
	// GetRepository is PasswordBroker::getRepository.
	GetRepository() TokenRepository
}

// TokenRepository is where a broker keeps its reset tokens -- a database table
// or a cache, depending on how the broker was built. This command needs one
// thing of it: throw away the records whose lifetime has passed.
//
// Answers the part of Illuminate\Auth\Passwords\TokenRepositoryInterface this
// command uses.
type TokenRepository interface {
	// DeleteExpired is TokenRepositoryInterface::deleteExpired. The context is
	// the fifth mechanical change (see hesape/auth's contracts.go): it is a
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

// The name, signature and description of the command, which are PHP's
// $signature and $description character for character.
const (
	// Name is the AsCommand attribute's name.
	Name = "auth:clear-resets"
	// Signature is Command::$signature.
	Signature = "auth:clear-resets {name? : The name of the password broker}"
	// Description is Command::$description, lowercased to match the rest of the
	// listing this console prints.
	Description = "flush expired password reset tokens"
)

// ClearResetsCommand is Illuminate\Auth\Console\ClearResetsCommand.
//
// It deletes the password reset tokens whose lifetime has passed. Run it from
// the scheduler; it is safe to run twice, and it takes no lock, because deleting
// an already deleted row is nothing.
type ClearResetsCommand struct {
	brokers BrokerFactory
}

// NewClearResetsCommand returns the command over a password broker factory.
func NewClearResetsCommand(brokers BrokerFactory) *ClearResetsCommand {
	return &ClearResetsCommand{brokers: brokers}
}

// Command is the value the console registry takes.
//
// It has no counterpart in PHP, where a command IS a class and the application
// finds it by scanning. hesape/console holds a slice of values instead, so that
// the listing and the compiler read the same registry -- see console.Command.
func (c *ClearResetsCommand) Command() console.Command {
	return console.Command{
		Signature:   Signature,
		Description: Description,
		Run:         c.Handle,
	}
}

// Handle executes the console command.
//
// It is ClearResetsCommand::handle:
//
//	$this->laravel['auth.password']->broker($this->argument('name'))->getRepository()->deleteExpired();
//	$this->components->info('Expired reset tokens cleared successfully.');
//
// The IO is the second parameter because PHP reads the argument off $this and Go
// has no such state on the command: the arguments arrive with the run.
//
// PHP returns void and lets an exception become a non-zero exit. The error is
// returned here, which is the same outcome through the console's own exit path.
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
