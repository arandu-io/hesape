package queue

import (
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/internal/linking"
)

// The tests are in the package because the registry is package state, and
// replacing it for each test keeps them independent of each other and of
// whatever a connector module would register on import.

// fakeConnector opens the null queue and remembers the endpoint it was handed,
// which is everything the registry is responsible for.
type fakeConnector struct {
	driver string
	opened *cache.Endpoint
}

func (c fakeConnector) Driver() string { return c.driver }

func (c fakeConnector) Open(e cache.Endpoint) (Queue, error) {
	if c.opened != nil {
		*c.opened = e
	}
	return NullQueue{}, nil
}

func freshRegistry(t *testing.T) {
	t.Helper()
	saved := connectors
	connectors = linking.NewRegistry[Connector]("queue")
	t.Cleanup(func() { connectors = saved })
}

func TestOpenUsesTheRegisteredConnector(t *testing.T) {
	freshRegistry(t)
	var opened cache.Endpoint
	Register(fakeConnector{driver: "redis", opened: &opened})

	q, err := Open("redis", cache.Endpoint{Address: "queue.internal:6379", Database: 2})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if q == nil {
		t.Fatal("Open returned no queue and no error")
	}
	if opened.Address != "queue.internal:6379" || opened.Database != 2 {
		t.Errorf("the connector was handed %+v, want the endpoint Open received", opened)
	}
}

func TestLinkedIsNilForALinkedDriver(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "redis"})

	if err := Linked("QUEUE_CONNECTION", "redis"); err != nil {
		t.Errorf("Linked = %v, want nil for a linked driver", err)
	}
}

// TestLinkedNamesTheMissingImport: the queue's connector is a module of its
// own, apart from the cache's, and the message has to name that one -- the
// cache module links no queue.
func TestLinkedNamesTheMissingImport(t *testing.T) {
	freshRegistry(t)

	err := Linked("QUEUE_CONNECTION", "redis")
	if err == nil {
		t.Fatal("Linked accepted a driver nothing registered")
	}

	want := "QUEUE_CONNECTION asks for redis and no connector for it is linked into this binary (linked: none).\n" +
		"Add it:\n\n" +
		"    go get github.com/arandu-io/hesape/queue/connectors/redis\n\n" +
		"and blank-import it in bootstrap/app.go, next to the other connectors:\n\n" +
		"    _ \"github.com/arandu-io/hesape/queue/connectors/redis\""
	if err.Error() != want {
		t.Errorf("message =\n%s\n\nwant\n%s", err, want)
	}
}

func TestLinkedListsWhatIsLinked(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "sqs"})
	Register(fakeConnector{driver: "beanstalkd"})

	err := Linked("QUEUE_CONNECTION", "redis")
	if err == nil || !strings.Contains(err.Error(), "(linked: beanstalkd, sqs).") {
		t.Errorf("Linked = %v, want it to list what is linked, sorted", err)
	}
}

// TestOpenOnAnUnlinkedDriverIsTheLinkedError: two calls answering the same
// question give the same answer, word for word.
func TestOpenOnAnUnlinkedDriverIsTheLinkedError(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "sqs"})

	q, err := Open("redis", cache.Endpoint{Address: "127.0.0.1:6379"})
	if err == nil {
		t.Fatal("Open succeeded for a driver nothing registered")
	}
	if q != nil {
		t.Errorf("Open returned a queue (%v) beside its error", q)
	}
	if want := Linked("QUEUE_CONNECTION", "redis"); err.Error() != want.Error() {
		t.Errorf("Open error =\n%s\n\nLinked error =\n%s", err, want)
	}
}

func TestTwoConnectorsForOneDriverPanic(t *testing.T) {
	freshRegistry(t)
	Register(fakeConnector{driver: "redis"})

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a second connector for the same driver was accepted")
		}
		if message, _ := recovered.(string); !strings.HasPrefix(message, "queue: ") || !strings.Contains(message, "remove one of the imports") {
			t.Errorf("the panic does not say whose registry it is and what to do: %v", recovered)
		}
	}()
	Register(fakeConnector{driver: "redis"})
}

// TestTheConnectorsOpenErrorReachesTheCaller: a connector that refuses an
// endpoint is the one that knows why.
func TestTheConnectorsOpenErrorReachesTheCaller(t *testing.T) {
	freshRegistry(t)
	Register(refusingConnector{})

	if _, err := Open("refusing", cache.Endpoint{}); !errors.Is(err, errRefused) {
		t.Errorf("Open = %v, want the connector's own error", err)
	}
}

var errRefused = errors.New("refused")

type refusingConnector struct{}

func (refusingConnector) Driver() string                     { return "refusing" }
func (refusingConnector) Open(cache.Endpoint) (Queue, error) { return nil, errRefused }
