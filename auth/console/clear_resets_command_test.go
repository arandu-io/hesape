package console_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	authconsole "github.com/arandu-io/hesape/auth/console"
	"github.com/arandu-io/hesape/console"
)

// repository records the sweep.
type repository struct {
	swept int
	err   error
}

func (r *repository) DeleteExpired(context.Context) error {
	r.swept++
	return r.err
}

type broker struct{ repository *repository }

func (b *broker) GetRepository() authconsole.TokenRepository { return b.repository }

// brokers records which broker was asked for, which is how the test proves the
// optional argument reached PasswordBrokerFactory::broker.
type brokers struct {
	asked      string
	repository *repository
	err        error
}

func (f *brokers) Broker(name string) (authconsole.Broker, error) {
	f.asked = name
	if f.err != nil {
		return nil, f.err
	}
	return &broker{repository: f.repository}, nil
}

// run executes the command with the given arguments and returns what it wrote.
func run(t *testing.T, c *authconsole.ClearResetsCommand, args ...string) (string, error) {
	t.Helper()

	out := &bytes.Buffer{}
	io := console.NewIO(authconsole.Name, args, out, out, nil)

	_, arguments, options, err := console.Parse(authconsole.Signature)
	if err != nil {
		t.Fatalf("the signature does not parse: %v", err)
	}
	input := console.NewInput(arguments, options)
	if err := input.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	io.SetInput(input)

	return out.String(), c.Handle(context.Background(), io)
}

func TestTheSweepRunsAndSaysSo(t *testing.T) {
	factory := &brokers{repository: &repository{}}
	command := authconsole.NewClearResetsCommand(factory)

	out := &bytes.Buffer{}
	io := console.NewIO(authconsole.Name, nil, out, out, nil)
	if err := command.Handle(context.Background(), io); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if factory.repository.swept != 1 {
		t.Fatalf("deleteExpired ran %d times, want once", factory.repository.swept)
	}
	if !strings.Contains(out.String(), "Expired reset tokens cleared successfully.") {
		t.Fatalf("output = %q, want Illuminate's sentence", out.String())
	}
}

// TestTheBrokerArgumentIsOptionalAndIsPassedOn is the whole of the signature:
// `{name? : The name of the password broker}`.
func TestTheBrokerArgumentIsOptionalAndIsPassedOn(t *testing.T) {
	factory := &brokers{repository: &repository{}}

	if _, err := run(t, authconsole.NewClearResetsCommand(factory)); err != nil {
		t.Fatalf("with no argument: %v", err)
	}
	if factory.asked != "" {
		t.Fatalf("broker = %q, want the default one", factory.asked)
	}

	if _, err := run(t, authconsole.NewClearResetsCommand(factory), "admins"); err != nil {
		t.Fatalf("with an argument: %v", err)
	}
	if factory.asked != "admins" {
		t.Fatalf("broker = %q, want admins", factory.asked)
	}
}

func TestAFailedSweepIsReported(t *testing.T) {
	wanted := errors.New("the database is unreachable")
	factory := &brokers{repository: &repository{err: wanted}}

	out := &bytes.Buffer{}
	err := authconsole.NewClearResetsCommand(factory).
		Handle(context.Background(), console.NewIO(authconsole.Name, nil, out, out, nil))

	if !errors.Is(err, wanted) {
		t.Fatalf("err = %v, want %v", err, wanted)
	}
	if strings.Contains(out.String(), "successfully") {
		t.Fatalf("output = %q: a failed sweep reported success", out.String())
	}
}

// TestACommandWithNoFactoryFails is the reason ErrNoBrokerFactory exists: a
// scheduled task that reports success while deleting nothing is a table that
// grows for a year before anybody looks.
func TestACommandWithNoFactoryFails(t *testing.T) {
	out := &bytes.Buffer{}
	err := authconsole.NewClearResetsCommand(nil).
		Handle(context.Background(), console.NewIO(authconsole.Name, nil, out, out, nil))

	if !errors.Is(err, authconsole.ErrNoBrokerFactory) {
		t.Fatalf("err = %v, want ErrNoBrokerFactory", err)
	}
}

// TestTheCommandValueCarriesIlluminatesSignature pins what the registry gets.
func TestTheCommandValueCarriesIlluminatesSignature(t *testing.T) {
	c := authconsole.NewClearResetsCommand(&brokers{repository: &repository{}}).Command()

	if c.Signature != authconsole.Signature {
		t.Fatalf("signature = %q", c.Signature)
	}
	if c.Description != authconsole.Description {
		t.Fatalf("description = %q", c.Description)
	}
	if c.Run == nil {
		t.Fatal("the command has no Run, so the registry would build a command nobody can call")
	}
}
