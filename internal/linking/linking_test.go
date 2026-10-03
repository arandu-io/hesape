package linking_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/internal/linking"
)

// TestTheMessageNamesTheSettingTheCommandAndTheLine is the text, whole. It is
// pinned byte for byte because three packages hand it to people, and a word
// that drifts here drifts in all three at once.
func TestTheMessageNamesTheSettingTheCommandAndTheLine(t *testing.T) {
	err := linking.NotLinked("CACHE_STORE", "redis", "github.com/arandu-io/hesape/redis", nil)

	want := "CACHE_STORE asks for redis and no connector for it is linked into this binary (linked: none).\n" +
		"Add it:\n" +
		"\n" +
		"    go get github.com/arandu-io/hesape/redis\n" +
		"\n" +
		"and blank-import it in bootstrap/app.go, next to the other connectors:\n" +
		"\n" +
		"    _ \"github.com/arandu-io/hesape/redis\""
	if err.Error() != want {
		t.Errorf("message =\n%s\n\nwant\n%s", err, want)
	}
}

// TestTheMessageListsWhatIsLinked: the gap between what was asked for and what
// is there is the diagnosis, so both halves are in the sentence.
func TestTheMessageListsWhatIsLinked(t *testing.T) {
	err := linking.NotLinked("QUEUE_CONNECTION", "redis", "github.com/arandu-io/hesape/queue/connectors/redis", []string{"beanstalkd", "sqs"})

	if !strings.HasPrefix(err.Error(), "QUEUE_CONNECTION asks for redis and no connector for it is linked into this binary (linked: beanstalkd, sqs).\n") {
		t.Errorf("the message does not list what is linked:\n%s", err)
	}
}

// TestAValueNoPackageProvidesNamesNoCommand: a `go get` for a module that does
// not exist is a second error stacked on the first.
func TestAValueNoPackageProvidesNamesNoCommand(t *testing.T) {
	err := linking.NotLinked("CACHE_STORE", "memcached", "", []string{"redis"})

	want := "CACHE_STORE asks for memcached and no connector for it is linked into this binary (linked: redis).\n" +
		"No package of github.com/arandu-io/hesape provides one."
	if err.Error() != want {
		t.Errorf("message =\n%s\n\nwant\n%s", err, want)
	}
}

func TestLookupFindsWhatWasRegistered(t *testing.T) {
	r := linking.NewRegistry[int]("test")
	r.Register("redis", 7)

	got, _, found := r.Lookup("redis")
	if !found || got != 7 {
		t.Fatalf("Lookup = %d, %v; want the registered connector", got, found)
	}
}

// TestAMissingNameReportsWhatIsThere: the names come back from the same read as
// the miss, so the error built from them describes the registry the lookup saw.
func TestAMissingNameReportsWhatIsThere(t *testing.T) {
	r := linking.NewRegistry[int]("test")
	r.Register("b", 2)
	r.Register("a", 1)

	_, names, found := r.Lookup("c")
	if found {
		t.Fatal("an unregistered name was found")
	}
	if strings.Join(names, ",") != "a,b" {
		t.Errorf("names = %v, want [a b], sorted", names)
	}
}

func TestNamesAreSorted(t *testing.T) {
	r := linking.NewRegistry[int]("test")
	for _, name := range []string{"c", "a", "b"} {
		r.Register(name, 0)
	}
	if got := strings.Join(r.Names(), ","); got != "a,b,c" {
		t.Errorf("names = %s, want a,b,c so two runs report the same thing", got)
	}
}

// TestASecondConnectorForOneNamePanics: two imports claiming one setting value
// is an import nobody meant to add, and the boot is where to find out.
func TestASecondConnectorForOneNamePanics(t *testing.T) {
	r := linking.NewRegistry[int]("cache")
	r.Register("redis", 1)

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a second connector for the same name was accepted")
		}
		message := fmt.Sprint(recovered)
		if !strings.HasPrefix(message, "cache: ") || !strings.Contains(message, "remove one of the imports") {
			t.Errorf("the panic does not say whose registry it is and what to do: %v", recovered)
		}
	}()
	r.Register("redis", 2)
}

// TestAnEmptyNamePanics: no setting can ask for it, so the connector would be
// linked and never used.
func TestAnEmptyNamePanics(t *testing.T) {
	r := linking.NewRegistry[int]("queue")

	defer func() {
		if recover() == nil {
			t.Fatal("a connector with no name was accepted")
		}
	}()
	r.Register("", 1)
}

// TestLookupsRaceRegistrations is for the race detector: a registry read while
// a late registration writes it must be guarded.
func TestLookupsRaceRegistrations(t *testing.T) {
	r := linking.NewRegistry[int]("test")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Register(fmt.Sprintf("driver-%d", i), i)
		}()
		go func() {
			defer wg.Done()
			_, _, _ = r.Lookup("driver-0")
			_ = r.Names()
		}()
	}
	wg.Wait()

	if got := len(r.Names()); got != 8 {
		t.Errorf("registered %d, want 8", got)
	}
}
