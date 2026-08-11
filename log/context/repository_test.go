package context_test

import (
	stdcontext "context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	logcontext "github.com/arandu-io/hesape/log/context"
	"github.com/arandu-io/hesape/log/context/events"
)

// dispatcher is the smallest thing that satisfies logcontext.Dispatcher: it
// keeps the listeners and hands every dispatched event to all of them, which is
// what Illuminate's dispatcher does once the class filtering is a type
// assertion.
type dispatcher struct {
	mu        sync.Mutex
	listeners []func(any)
	fired     []any
}

func (d *dispatcher) Dispatch(event any) {
	d.mu.Lock()
	// A copy, so that a listener registering another does not write the slice
	// this loop is walking.
	listeners := make([]func(any), len(d.listeners))
	copy(listeners, d.listeners)
	d.fired = append(d.fired, event)
	d.mu.Unlock()

	for _, listen := range listeners {
		listen(event)
	}
}

func (d *dispatcher) Listen(listener func(any)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, listener)
}

func (d *dispatcher) events() []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]any(nil), d.fired...)
}

func newRepository() *logcontext.Repository { return logcontext.New(&dispatcher{}) }

func TestIntoAndForCarryTheRepository(t *testing.T) {
	repository := newRepository()
	ctx := logcontext.Into(stdcontext.Background(), repository)

	if logcontext.For(ctx) != repository {
		t.Fatal("For did not return the repository Into stored")
	}
	if logcontext.For(stdcontext.Background()) != nil {
		t.Fatal("For on a bare context returned something")
	}
	if logcontext.For(nil) != nil { //nolint:staticcheck // a nil context is exactly the case under test
		t.Fatal("For on a nil context returned something")
	}
}

// TestHasSeesAKeyThatGetDoesNot is the split PHP has between array_key_exists
// and ??: add('key', null) is present for has and absent for get.
func TestHasSeesAKeyThatGetDoesNot(t *testing.T) {
	repository := newRepository().Add("key", nil)

	if !repository.Has("key") {
		t.Fatal("Has said no about a key that was added with a nil value")
	}
	if repository.Missing("key") {
		t.Fatal("Missing said yes about a key that is there")
	}
	if got := repository.Get("key", "fallback"); got != "fallback" {
		t.Fatalf("Get on a nil value returned %v, want the default", got)
	}
}

func TestGetDefaultIsResolvedOnlyWhenNeeded(t *testing.T) {
	repository := newRepository().Add("present", "value")

	called := false
	if got := repository.Get("present", func() any { called = true; return "no" }); got != "value" {
		t.Fatalf("Get returned %v, want the stored value", got)
	}
	if called {
		t.Fatal("the default closure ran even though the key was there")
	}
	if got := repository.Get("absent", func() any { return "yes" }); got != "yes" {
		t.Fatalf("Get returned %v, want the resolved default", got)
	}
	if got := repository.Get("absent"); got != nil {
		t.Fatalf("Get with no default returned %v, want nil", got)
	}
}

func TestAddTakesAMapOrAPair(t *testing.T) {
	repository := newRepository().
		Add("one", 1).
		Add(map[string]any{"two": 2, "three": 3}).
		Add("bare")

	want := map[string]any{"one": 1, "two": 2, "three": 3, "bare": nil}
	if got := repository.All(); !reflect.DeepEqual(got, want) {
		t.Fatalf("All returned %v, want %v", got, want)
	}
}

func TestAllIsACopy(t *testing.T) {
	repository := newRepository().Add("key", "value")

	all := repository.All()
	all["key"] = "changed"
	all["new"] = "added"

	if got := repository.Get("key"); got != "value" {
		t.Fatalf("writing into the map All returned changed the repository: %v", got)
	}
	if repository.Has("new") {
		t.Fatal("a key added to the map All returned reached the repository")
	}
}

func TestHiddenIsASeparateHalf(t *testing.T) {
	repository := newRepository().AddHidden("secret", "value")

	if repository.Has("secret") {
		t.Fatal("a hidden key showed up in the visible half")
	}
	if !repository.HasHidden("secret") {
		t.Fatal("HasHidden said no about a hidden key")
	}
	if repository.MissingHidden("secret") {
		t.Fatal("MissingHidden said yes about a hidden key that is there")
	}
	if got := repository.GetHidden("secret"); got != "value" {
		t.Fatalf("GetHidden returned %v", got)
	}
	if got := repository.AllHidden(); !reflect.DeepEqual(got, map[string]any{"secret": "value"}) {
		t.Fatalf("AllHidden returned %v", got)
	}
}

func TestAddIfDoesNotOverwrite(t *testing.T) {
	repository := newRepository().
		Add("key", "first").
		AddIf("key", "second").
		AddIf("fresh", "value").
		AddHidden("hidden", "first").
		AddHiddenIf("hidden", "second").
		AddHiddenIf("freshHidden", "value")

	if got := repository.Get("key"); got != "first" {
		t.Fatalf("AddIf overwrote an existing key: %v", got)
	}
	if got := repository.Get("fresh"); got != "value" {
		t.Fatalf("AddIf did not add a missing key: %v", got)
	}
	if got := repository.GetHidden("hidden"); got != "first" {
		t.Fatalf("AddHiddenIf overwrote an existing key: %v", got)
	}
	if got := repository.GetHidden("freshHidden"); got != "value" {
		t.Fatalf("AddHiddenIf did not add a missing key: %v", got)
	}
}

// TestAddIfCountsANilValueAsPresent follows has(), which is array_key_exists:
// PHP's addIf asks has(), not get(), so a nil value blocks the write.
func TestAddIfCountsANilValueAsPresent(t *testing.T) {
	repository := newRepository().Add("key", nil).AddIf("key", "value")

	if got := repository.Get("key"); got != nil {
		t.Fatalf("AddIf wrote over a key that held nil: %v", got)
	}
}

func TestRememberReturnsTheExistingValue(t *testing.T) {
	repository := newRepository().Add("key", "first")

	called := false
	if got := repository.Remember("key", func() any { called = true; return "second" }); got != "first" {
		t.Fatalf("Remember returned %v, want the stored value", got)
	}
	if called {
		t.Fatal("Remember resolved the closure for a key that was already there")
	}
	if got := repository.Remember("fresh", func() any { return "made" }); got != "made" {
		t.Fatalf("Remember returned %v, want the resolved value", got)
	}
	if got := repository.Get("fresh"); got != "made" {
		t.Fatalf("Remember did not store what it returned: %v", got)
	}
	if got := repository.RememberHidden("hidden", "made"); got != "made" {
		t.Fatalf("RememberHidden returned %v", got)
	}
	if got := repository.GetHidden("hidden"); got != "made" {
		t.Fatalf("RememberHidden did not store what it returned: %v", got)
	}
}

func TestPullForgetsWhatItReturns(t *testing.T) {
	repository := newRepository().Add("key", "value").AddHidden("hidden", "value")

	if got := repository.Pull("key"); got != "value" {
		t.Fatalf("Pull returned %v", got)
	}
	if repository.Has("key") {
		t.Fatal("Pull left the key behind")
	}
	if got := repository.Pull("key", "gone"); got != "gone" {
		t.Fatalf("Pull on a missing key returned %v, want the default", got)
	}
	if got := repository.PullHidden("hidden"); got != "value" {
		t.Fatalf("PullHidden returned %v", got)
	}
	if repository.HasHidden("hidden") {
		t.Fatal("PullHidden left the key behind")
	}
}

func TestOnlyAndExceptSkipKeysThatAreNotThere(t *testing.T) {
	repository := newRepository().
		Add(map[string]any{"a": 1, "b": 2, "c": 3}).
		AddHidden(map[string]any{"a": 1, "b": 2})

	if got := repository.Only([]string{"a", "c", "missing"}); !reflect.DeepEqual(got, map[string]any{"a": 1, "c": 3}) {
		t.Fatalf("Only returned %v", got)
	}
	if got := repository.Except([]string{"a", "missing"}); !reflect.DeepEqual(got, map[string]any{"b": 2, "c": 3}) {
		t.Fatalf("Except returned %v", got)
	}
	if got := repository.OnlyHidden([]string{"b", "missing"}); !reflect.DeepEqual(got, map[string]any{"b": 2}) {
		t.Fatalf("OnlyHidden returned %v", got)
	}
	if got := repository.ExceptHidden([]string{"b"}); !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Fatalf("ExceptHidden returned %v", got)
	}
	if got := repository.Only(nil); len(got) != 0 {
		t.Fatalf("Only with no key returned %v, want nothing", got)
	}
}

func TestForgetTakesSeveralKeysAndIgnoresTheMissing(t *testing.T) {
	repository := newRepository().
		Add(map[string]any{"a": 1, "b": 2, "c": 3}).
		AddHidden(map[string]any{"a": 1})

	repository.Forget("a", "b", "never-was")
	if got := repository.All(); !reflect.DeepEqual(got, map[string]any{"c": 3}) {
		t.Fatalf("Forget left %v", got)
	}
	repository.ForgetHidden("a", "never-was")
	if got := repository.AllHidden(); len(got) != 0 {
		t.Fatalf("ForgetHidden left %v", got)
	}
	// No key at all is PHP's (array) null, which forgets nothing.
	repository.Forget()
	if got := repository.All(); !reflect.DeepEqual(got, map[string]any{"c": 3}) {
		t.Fatalf("Forget with no key changed the context to %v", got)
	}
}

func TestPushCreatesTheStack(t *testing.T) {
	repository := newRepository()

	if _, err := repository.Push("breadcrumbs", "one", "two"); err != nil {
		t.Fatalf("Push onto a missing key: %v", err)
	}
	if _, err := repository.Push("breadcrumbs", "three"); err != nil {
		t.Fatalf("Push onto an existing stack: %v", err)
	}
	want := []any{"one", "two", "three"}
	if got := repository.Get("breadcrumbs"); !reflect.DeepEqual(got, want) {
		t.Fatalf("the stack is %v, want %v", got, want)
	}
	// Nothing to push is not an error: PHP's ...$values accepts none.
	if _, err := repository.Push("breadcrumbs"); err != nil {
		t.Fatalf("Push with no value: %v", err)
	}
	if got := repository.Get("breadcrumbs"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Push with no value changed the stack to %v", got)
	}
}

func TestPushOntoSomethingThatIsNotAStack(t *testing.T) {
	for name, seed := range map[string]any{"a string": "value", "a nil": nil, "a map": map[string]any{}} {
		t.Run(name, func(t *testing.T) {
			repository := newRepository().Add("key", seed)
			if _, err := repository.Push("key", "one"); !errors.Is(err, logcontext.ErrUnableToPush) {
				t.Fatalf("Push returned %v, want ErrUnableToPush", err)
			}
			repository.AddHidden("key", seed)
			if _, err := repository.PushHidden("key", "one"); !errors.Is(err, logcontext.ErrUnableToPush) {
				t.Fatalf("PushHidden returned %v, want ErrUnableToPush", err)
			}
		})
	}
}

func TestPopTakesTheLatestValue(t *testing.T) {
	repository := newRepository()
	if _, err := repository.Push("stack", "one", "two"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := repository.Pop("stack")
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got != "two" {
		t.Fatalf("Pop returned %v, want the last value", got)
	}
	if left := repository.Get("stack"); !reflect.DeepEqual(left, []any{"one"}) {
		t.Fatalf("the stack is %v after the pop", left)
	}
}

func TestPopOnAnEmptyOrAbsentStack(t *testing.T) {
	repository := newRepository()

	if _, err := repository.Pop("never-was"); !errors.Is(err, logcontext.ErrUnableToPop) {
		t.Fatalf("Pop on a missing key returned %v, want ErrUnableToPop", err)
	}
	repository.Add("empty", []any{})
	if _, err := repository.Pop("empty"); !errors.Is(err, logcontext.ErrUnableToPop) {
		t.Fatalf("Pop on an empty stack returned %v, want ErrUnableToPop", err)
	}
	repository.Add("scalar", 1)
	if _, err := repository.Pop("scalar"); !errors.Is(err, logcontext.ErrUnableToPop) {
		t.Fatalf("Pop on a scalar returned %v, want ErrUnableToPop", err)
	}
	if _, err := repository.PopHidden("never-was"); !errors.Is(err, logcontext.ErrUnableToPop) {
		t.Fatalf("PopHidden on a missing key returned %v, want ErrUnableToPop", err)
	}
}

func TestHiddenStackIsSeparate(t *testing.T) {
	repository := newRepository()
	if _, err := repository.PushHidden("stack", "one", "two"); err != nil {
		t.Fatalf("PushHidden: %v", err)
	}
	if repository.Has("stack") {
		t.Fatal("PushHidden wrote into the visible half")
	}

	got, err := repository.PopHidden("stack")
	if err != nil {
		t.Fatalf("PopHidden: %v", err)
	}
	if got != "two" {
		t.Fatalf("PopHidden returned %v", got)
	}
}

func TestIncrementStartsAtZero(t *testing.T) {
	repository := newRepository()

	repository.Increment("count")
	if got := repository.Get("count"); got != 1 {
		t.Fatalf("Increment on a missing key gave %v, want 1", got)
	}
	repository.Increment("count", 4)
	if got := repository.Get("count"); got != 5 {
		t.Fatalf("Increment by 4 gave %v, want 5", got)
	}
	repository.Decrement("count")
	if got := repository.Get("count"); got != 4 {
		t.Fatalf("Decrement gave %v, want 4", got)
	}
	repository.Decrement("count", 10)
	if got := repository.Get("count"); got != -6 {
		t.Fatalf("Decrement by 10 gave %v, want -6", got)
	}
}

// TestIncrementCastsWhatItFinds is PHP's (int) cast: a numeric string counts,
// anything else restarts from zero rather than failing.
func TestIncrementCastsWhatItFinds(t *testing.T) {
	repository := newRepository().Add("numeric", "7").Add("words", "seven")

	repository.Increment("numeric")
	if got := repository.Get("numeric"); got != 8 {
		t.Fatalf("Increment over the numeric string gave %v, want 8", got)
	}
	repository.Increment("words")
	if got := repository.Get("words"); got != 1 {
		t.Fatalf("Increment over a non-number gave %v, want 1", got)
	}
}

func TestStackContains(t *testing.T) {
	repository := newRepository()
	if _, err := repository.Push("stack", 1, "two"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	cases := []struct {
		name   string
		value  any
		strict []bool
		want   bool
	}{
		{"the value is there", "two", nil, true},
		{"the value is not there", "three", nil, false},
		{"loose compares a number to its string", "1", nil, true},
		{"strict does not", "1", []bool{true}, false},
		{"strict matches the same type", 1, []bool{true}, true},
		{"a closure decides", func(item any) bool { return item == "two" }, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := repository.StackContains("stack", c.value, c.strict...)
			if err != nil {
				t.Fatalf("StackContains: %v", err)
			}
			if got != c.want {
				t.Fatalf("StackContains returned %v, want %v", got, c.want)
			}
		})
	}
}

func TestStackContainsOnAKeyThatIsNotAStack(t *testing.T) {
	repository := newRepository().Add("scalar", 1).AddHidden("scalar", 1)

	if _, err := repository.StackContains("scalar", 1); !errors.Is(err, logcontext.ErrNotAStack) {
		t.Fatalf("StackContains returned %v, want ErrNotAStack", err)
	}
	if _, err := repository.HiddenStackContains("scalar", 1); !errors.Is(err, logcontext.ErrNotAStack) {
		t.Fatalf("HiddenStackContains returned %v, want ErrNotAStack", err)
	}
	// A key that is not there is stackable, and the answer is simply no.
	got, err := repository.StackContains("never-was", 1)
	if err != nil {
		t.Fatalf("StackContains on a missing key: %v", err)
	}
	if got {
		t.Fatal("StackContains said yes about a key that is not there")
	}
}

func TestScopeRestoresTheContext(t *testing.T) {
	repository := newRepository().Add("kept", "before").AddHidden("hidden", "before")

	err := repository.Scope(func() error {
		if got := repository.Get("kept"); got != "during" {
			t.Fatalf("inside the scope the value is %v", got)
		}
		if got := repository.GetHidden("hidden"); got != "during" {
			t.Fatalf("inside the scope the hidden value is %v", got)
		}
		repository.Add("added-inside", true)
		return nil
	}, map[string]any{"kept": "during"}, map[string]any{"hidden": "during"})
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}

	if got := repository.Get("kept"); got != "before" {
		t.Fatalf("after the scope the value is %v, want the one from before", got)
	}
	if got := repository.GetHidden("hidden"); got != "before" {
		t.Fatalf("after the scope the hidden value is %v", got)
	}
	if repository.Has("added-inside") {
		t.Fatal("a key added inside the scope survived it")
	}
}

// TestScopeRestoresAfterAFailure is PHP's finally: the restore is not
// conditional on the callback returning normally.
func TestScopeRestoresAfterAFailure(t *testing.T) {
	repository := newRepository().Add("kept", "before")
	failure := errors.New("the callback failed")

	if err := repository.Scope(func() error { return failure }, map[string]any{"kept": "during"}, nil); !errors.Is(err, failure) {
		t.Fatalf("Scope returned %v, want the callback's error", err)
	}
	if got := repository.Get("kept"); got != "before" {
		t.Fatalf("after a failing scope the value is %v", got)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not come back out of Scope")
			}
		}()
		_ = repository.Scope(func() error { panic("boom") }, map[string]any{"kept": "during"}, nil)
	}()
	if got := repository.Get("kept"); got != "before" {
		t.Fatalf("after a panicking scope the value is %v", got)
	}
}

func TestWhenRunsOneSideOrTheOther(t *testing.T) {
	repository := newRepository()

	repository.When(true, func(r *logcontext.Repository) { r.Add("yes", 1) }, func(r *logcontext.Repository) { r.Add("no", 1) })
	repository.When(false, func(r *logcontext.Repository) { r.Add("wrong", 1) }, func(r *logcontext.Repository) { r.Add("otherwise", 1) })
	repository.When(func(r *logcontext.Repository) bool { return r.Has("yes") }, func(r *logcontext.Repository) { r.Add("closure", 1) })
	repository.When("", func(r *logcontext.Repository) { r.Add("empty-string", 1) })
	repository.When(0, func(r *logcontext.Repository) { r.Add("zero", 1) })

	want := map[string]any{"yes": 1, "otherwise": 1, "closure": 1}
	if got := repository.All(); !reflect.DeepEqual(got, want) {
		t.Fatalf("When produced %v, want %v", got, want)
	}
}

func TestFlushAndIsEmpty(t *testing.T) {
	repository := newRepository()
	if !repository.IsEmpty() {
		t.Fatal("a fresh repository is not empty")
	}
	repository.AddHidden("hidden", 1)
	if repository.IsEmpty() {
		t.Fatal("a repository with only a hidden value reported empty")
	}
	repository.Add("visible", 1).Flush()
	if !repository.IsEmpty() {
		t.Fatalf("Flush left %v and %v", repository.All(), repository.AllHidden())
	}
}

func TestDehydrateReturnsNothingWhenThereIsNothing(t *testing.T) {
	dehydrated, err := newRepository().Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}
	if dehydrated != nil {
		t.Fatalf("Dehydrate on an empty repository returned %v, want nil", dehydrated)
	}
}

func TestDehydrateAndHydrateRoundTrip(t *testing.T) {
	source := newRepository().Add("order", "abc").AddHidden("token", "secret")

	dehydrated, err := source.Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}

	target := newRepository().Add("stale", "gone")
	if err := target.Hydrate(dehydrated); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}

	if got := target.Get("order"); got != "abc" {
		t.Fatalf("the hydrated value is %v", got)
	}
	if got := target.GetHidden("token"); got != "secret" {
		t.Fatalf("the hydrated hidden value is %v", got)
	}
	if target.Has("stale") {
		t.Fatal("Hydrate did not flush what the repository held before")
	}
}

// TestHydrateOfNothingIsNotAFailure is PHP's `$context['data'] ?? []`: the
// queue calls hydrate with null when the payload carried no context.
func TestHydrateOfNothingIsNotAFailure(t *testing.T) {
	repository := newRepository().Add("stale", 1)

	if err := repository.Hydrate(nil); err != nil {
		t.Fatalf("Hydrate(nil): %v", err)
	}
	if !repository.IsEmpty() {
		t.Fatalf("Hydrate(nil) left %v", repository.All())
	}
}

func TestDehydratingCanStillChangeWhatTravels(t *testing.T) {
	repository := logcontext.New(&dispatcher{}).Add("order", "abc")

	repository.Dehydrating(func(r *logcontext.Repository) {
		r.Add("added-on-the-way-out", true)
		r.Forget("order")
	})

	dehydrated, err := repository.Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}

	target := logcontext.New(&dispatcher{})
	if err := target.Hydrate(dehydrated); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if target.Has("order") {
		t.Fatal("the listener removed a key and it travelled anyway")
	}
	if got := target.Get("added-on-the-way-out"); got != true {
		t.Fatalf("the key the listener added did not travel: %v", got)
	}
	// The live repository is untouched: the event carries the copy.
	if !repository.Has("order") {
		t.Fatal("the listener wrote through to the live repository")
	}
}

func TestHydratedRunsAfterTheValuesAreBack(t *testing.T) {
	dehydrated, err := newRepository().Add("order", "abc").Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}

	target := logcontext.New(&dispatcher{})
	seen := ""
	target.Hydrated(func(r *logcontext.Repository) {
		if value, ok := r.Get("order").(string); ok {
			seen = value
		}
	})
	if err := target.Hydrate(dehydrated); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if seen != "abc" {
		t.Fatalf("the hydrated listener saw %q", seen)
	}
}

func TestTheTwoEventsAreDispatched(t *testing.T) {
	bus := &dispatcher{}
	repository := logcontext.New(bus).Add("order", "abc")

	dehydrated, err := repository.Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}
	if err := repository.Hydrate(dehydrated); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}

	fired := bus.events()
	if len(fired) != 2 {
		t.Fatalf("%d events were dispatched, want 2", len(fired))
	}
	dehydrating, ok := fired[0].(events.ContextDehydrating)
	if !ok {
		t.Fatalf("the first event is %T, want ContextDehydrating", fired[0])
	}
	if got := dehydrating.Context.All(); !reflect.DeepEqual(got, map[string]any{"order": "abc"}) {
		t.Fatalf("ContextDehydrating carried %v", got)
	}
	if _, ok := fired[1].(events.ContextHydrated); !ok {
		t.Fatalf("the second event is %T, want ContextHydrated", fired[1])
	}
}

func TestHydrateReportsAValueItCannotReadBack(t *testing.T) {
	repository := newRepository()

	err := repository.Hydrate(map[string]any{"data": map[string]any{"broken": "{not json"}})
	if err == nil {
		t.Fatal("Hydrate accepted a value it cannot read back")
	}
}

// TestHandleUnserializeExceptionsUsing is the static hook, so this test does
// not run in parallel with anything and puts the default back on the way out.
func TestHandleUnserializeExceptionsUsing(t *testing.T) {
	repository := newRepository()
	t.Cleanup(func() { repository.HandleUnserializeExceptionsUsing(nil) })

	var seenKey string
	var seenHidden bool
	repository.HandleUnserializeExceptionsUsing(func(err error, key string, value any, hidden bool) any {
		seenKey, seenHidden = key, hidden
		return "replaced"
	})

	if err := repository.Hydrate(map[string]any{"hidden": map[string]any{"broken": "{not json"}}); err != nil {
		t.Fatalf("Hydrate with a handler installed: %v", err)
	}
	if seenKey != "broken" {
		t.Fatalf("the handler saw the key %q", seenKey)
	}
	if !seenHidden {
		t.Fatal("the handler was not told the value was hidden")
	}
	if got := repository.GetHidden("broken"); got != "replaced" {
		t.Fatalf("the replacement value is %v", got)
	}
}

func TestDehydrateReportsAValueItCannotWriteDown(t *testing.T) {
	repository := newRepository().Add("channel", make(chan int))

	if _, err := repository.Dehydrate(); err == nil {
		t.Fatal("Dehydrate accepted a value that cannot be serialised")
	}
}

// TestEveryMethodIsSafeOnANilRepository is the contract For's documentation
// makes: a caller that got nil out of a context does not have to check.
func TestEveryMethodIsSafeOnANilRepository(t *testing.T) {
	var repository *logcontext.Repository

	if repository.Has("k") || repository.HasHidden("k") {
		t.Fatal("a nil repository claimed to hold something")
	}
	if !repository.Missing("k") || !repository.MissingHidden("k") || !repository.IsEmpty() {
		t.Fatal("a nil repository is not empty")
	}
	if got := repository.Get("k", "default"); got != "default" {
		t.Fatalf("Get on a nil repository returned %v", got)
	}
	if got := repository.GetHidden("k", "default"); got != "default" {
		t.Fatalf("GetHidden on a nil repository returned %v", got)
	}
	if got := repository.Pull("k", "default"); got != "default" {
		t.Fatalf("Pull on a nil repository returned %v", got)
	}
	if got := repository.PullHidden("k", "default"); got != "default" {
		t.Fatalf("PullHidden on a nil repository returned %v", got)
	}
	if got := repository.Remember("k", "value"); got != "value" {
		t.Fatalf("Remember on a nil repository returned %v", got)
	}
	if got := repository.RememberHidden("k", "value"); got != "value" {
		t.Fatalf("RememberHidden on a nil repository returned %v", got)
	}
	for name, got := range map[string]map[string]any{
		"All":          repository.All(),
		"AllHidden":    repository.AllHidden(),
		"Only":         repository.Only([]string{"k"}),
		"OnlyHidden":   repository.OnlyHidden([]string{"k"}),
		"Except":       repository.Except([]string{"k"}),
		"ExceptHidden": repository.ExceptHidden([]string{"k"}),
	} {
		if len(got) != 0 {
			t.Fatalf("%s on a nil repository returned %v", name, got)
		}
	}
	repository.Add("k", 1).AddHidden("k", 1).AddIf("k", 1).AddHiddenIf("k", 1).
		Forget("k").ForgetHidden("k").Increment("k").Decrement("k").Flush().
		Dehydrating(func(*logcontext.Repository) {}).Hydrated(func(*logcontext.Repository) {})
	if _, err := repository.Push("k", 1); err == nil {
		t.Fatal("Push on a nil repository reported success")
	}
	if _, err := repository.PushHidden("k", 1); err == nil {
		t.Fatal("PushHidden on a nil repository reported success")
	}
	if _, err := repository.Pop("k"); err == nil {
		t.Fatal("Pop on a nil repository reported success")
	}
	if _, err := repository.PopHidden("k"); err == nil {
		t.Fatal("PopHidden on a nil repository reported success")
	}
	if _, err := repository.StackContains("k", 1); err == nil {
		t.Fatal("StackContains on a nil repository reported success")
	}
	if _, err := repository.HiddenStackContains("k", 1); err == nil {
		t.Fatal("HiddenStackContains on a nil repository reported success")
	}
	if err := repository.Scope(func() error { return nil }, nil, nil); err != nil {
		t.Fatalf("Scope on a nil repository: %v", err)
	}
	if err := repository.Hydrate(nil); err != nil {
		t.Fatalf("Hydrate on a nil repository: %v", err)
	}
	if dehydrated, err := repository.Dehydrate(); err != nil || dehydrated != nil {
		t.Fatalf("Dehydrate on a nil repository returned %v, %v", dehydrated, err)
	}
}

// TestARepositoryWithoutADispatcherStillHoldsContext: the dispatcher is
// optional, and only the four event-shaped calls have nobody to tell.
func TestARepositoryWithoutADispatcherStillHoldsContext(t *testing.T) {
	repository := logcontext.New(nil).Add("key", "value")

	if got := repository.Get("key"); got != "value" {
		t.Fatalf("Get returned %v", got)
	}
	repository.Dehydrating(func(*logcontext.Repository) { t.Error("a listener ran without a dispatcher") })
	dehydrated, err := repository.Dehydrate()
	if err != nil {
		t.Fatalf("Dehydrate without a dispatcher: %v", err)
	}
	if err := repository.Hydrate(dehydrated); err != nil {
		t.Fatalf("Hydrate without a dispatcher: %v", err)
	}
	if got := repository.Get("key"); got != "value" {
		t.Fatalf("after the round trip the value is %v", got)
	}
}

func TestTheRepositoryIsSafeUnderConcurrentUse(t *testing.T) {
	repository := newRepository()

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i%4)
			repository.Add(key, i)
			repository.Increment("count")
			_, _ = repository.Push("stack", i)
			_ = repository.All()
			_ = repository.Only([]string{key})
			repository.Forget("never-was")
		}()
	}
	wg.Wait()

	if got := repository.Get("count"); got != 32 {
		t.Fatalf("the counter ended at %v, want 32", got)
	}
	stack, ok := repository.Get("stack").([]any)
	if !ok || len(stack) != 32 {
		t.Fatalf("the stack holds %v", repository.Get("stack"))
	}
}
