package fakes

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// recorder captures what an assertion reported instead of failing the test, so
// that a test can check the failure message itself. testing.TB cannot be
// implemented outside the testing package, which is why the assertions take
// TestingT.
type recorder struct {
	failures []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *recorder) failed() bool { return len(r.failures) > 0 }

func (r *recorder) message() string { return strings.Join(r.failures, "\n") }

// assertPasses fails the running test when the assertion under test reported
// anything.
func assertPasses(t *testing.T, r *recorder) {
	t.Helper()
	if r.failed() {
		t.Fatalf("expected the assertion to pass, it reported:\n%s", r.message())
	}
}

// assertFails fails the running test unless the assertion under test reported a
// failure naming every one of the given fragments. The fragments are what the
// message has to say for it to be worth reading.
func assertFails(t *testing.T, r *recorder, fragments ...string) {
	t.Helper()
	if !r.failed() {
		t.Fatalf("expected the assertion to fail, it passed")
	}
	message := r.message()
	for _, fragment := range fragments {
		if !strings.Contains(message, fragment) {
			t.Errorf("expected the failure to mention %q, it said:\n%s", fragment, message)
		}
	}
}

func TestPluralAgreesWithTheCount(t *testing.T) {
	t.Parallel()

	if got := plural("time", 1); got != "time" {
		t.Errorf("plural(time, 1) = %q, want time", got)
	}
	if got := plural("time", 0); got != "times" {
		t.Errorf("plural(time, 0) = %q, want times", got)
	}
	if got := countedWere(1, "job"); got != "1 job was" {
		t.Errorf("countedWere(1, job) = %q, want 1 job was", got)
	}
	if got := countedWere(0, "job"); got != "0 jobs were" {
		t.Errorf("countedWere(0, job) = %q, want 0 jobs were", got)
	}
	if got := countedAre(1, "listener"); got != "1 listener is" {
		t.Errorf("countedAre(1, listener) = %q, want 1 listener is", got)
	}
}

func TestClassNamesKeepsTheOrderOfFirstRecord(t *testing.T) {
	t.Parallel()

	values := []any{sampleJob{}, otherJob{}, sampleJob{}}
	got := classNames(values)
	want := []string{"fakes.sampleJob", "fakes.otherJob"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("classNames = %v, want %v", got, want)
	}
}

func TestUUIDsAreWellFormedAndOrdered(t *testing.T) {
	t.Parallel()

	id := uuid()
	if len(id) != 36 || id[14] != '4' {
		t.Errorf("uuid() = %q, want a 36 character version 4 uuid", id)
	}

	// Ordered means ordered even inside one millisecond, which is where a
	// test that dispatches batches in a loop lives.
	previous := ""
	for i := 0; i < 200; i++ {
		id := orderedUUID()
		if len(id) != 36 || id[14] != '7' {
			t.Fatalf("orderedUUID() = %q, want a 36 character version 7 uuid", id)
		}
		if previous != "" && id <= previous {
			t.Fatalf("orderedUUID() gave %q then %q, want the second to sort after the first", previous, id)
		}
		previous = id
	}
}

func TestRestoreSurvivesAValueItCannotMarshal(t *testing.T) {
	t.Parallel()

	// A value holding a func cannot be marshalled, and the round trip has to
	// hand it back rather than drop it: losing the record would fail an
	// assertion for a reason that has nothing to do with what is being checked.
	job := &CallQueuedClosure{Closure: func() {}}
	if got := restore(job); got != any(job) {
		t.Errorf("restore of an unmarshallable value = %v, want the value itself", got)
	}

	restored := restore(sampleJob{Name: "a"})
	if !reflect.DeepEqual(restored, sampleJob{Name: "a"}) {
		t.Errorf("restore(sampleJob{a}) = %v, want an equal value", restored)
	}
}
