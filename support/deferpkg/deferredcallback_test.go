package deferpkg

import "testing"

func TestACallbackWithNoNameStillGetsOne(t *testing.T) {
	first := NewDeferredCallback(func() {}, "", false)
	second := NewDeferredCallback(func() {}, "", false)

	if first.GetName() == "" || second.GetName() == "" {
		t.Fatal("a callback with no name cannot be called off")
	}
	if first.GetName() == second.GetName() {
		t.Fatal("two callbacks were given the same name")
	}
}

func TestNameAndAlwaysAreWriters(t *testing.T) {
	callback := NewDeferredCallback(func() {}, "first", false)

	callback.Name("second").Always()
	if callback.GetName() != "second" || !callback.GetAlways() {
		t.Fatalf("got %q, always %v", callback.GetName(), callback.GetAlways())
	}
	if callback.Always(false).GetAlways() {
		t.Fatal("always(false) is the other way")
	}
}

func TestInvokeRunsEveryCallbackAndEmptiesTheCollection(t *testing.T) {
	collection := NewDeferredCallbackCollection()
	ran := []string{}

	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "first") }, "first", false))
	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "second") }, "second", false))

	if collection.Count() != 2 {
		t.Fatalf("got %d", collection.Count())
	}

	collection.Invoke()
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Fatalf("got %v", ran)
	}
	if collection.Count() != 0 {
		t.Fatalf("the collection kept %d callbacks", collection.Count())
	}
}

func TestOfTwoCallbacksUnderOneNameTheLaterOneStands(t *testing.T) {
	collection := NewDeferredCallbackCollection()
	ran := []string{}

	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "first") }, "welcome", false))
	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "second") }, "welcome", false))

	if collection.Count() != 1 {
		t.Fatalf("got %d", collection.Count())
	}

	collection.Invoke()
	if len(ran) != 1 || ran[0] != "second" {
		t.Fatalf("got %v", ran)
	}
}

func TestForgetDropsTheCallbacksUnderTheName(t *testing.T) {
	collection := NewDeferredCallbackCollection()
	ran := false

	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = true }, "welcome", false))
	collection.Forget("welcome")

	collection.Invoke()
	if ran {
		t.Fatal("a forgotten callback ran")
	}
	if collection.Count() != 0 {
		t.Fatalf("got %d", collection.Count())
	}
}

func TestInvokeWhenRunsOnlyWhatTheTestLetsThrough(t *testing.T) {
	collection := NewDeferredCallbackCollection()
	ran := []string{}

	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "always") }, "always", true))
	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = append(ran, "only ok") }, "only ok", false))

	collection.InvokeWhen(func(c *DeferredCallback) bool { return c.GetAlways() })

	if len(ran) != 1 || ran[0] != "always" {
		t.Fatalf("got %v", ran)
	}
	if collection.Count() != 0 {
		t.Fatal("a callback the test refused is still dropped, as the PHP unsets it")
	}
}

func TestACallbackThatBlowsUpDoesNotTakeTheOthersDown(t *testing.T) {
	collection := NewDeferredCallbackCollection()
	ran := false

	collection.OffsetSet(-1, NewDeferredCallback(func() { panic("boom") }, "boom", false))
	collection.OffsetSet(-1, NewDeferredCallback(func() { ran = true }, "after", false))

	collection.Invoke()
	if !ran {
		t.Fatal("the callback after the one that blew up never ran")
	}
}

func TestFirstAndTheOffsetsOnAnEmptyCollection(t *testing.T) {
	collection := NewDeferredCallbackCollection()

	if collection.First() != nil {
		t.Fatal("an empty collection has no first callback")
	}
	if collection.OffsetExists(0) || collection.OffsetGet(0) != nil {
		t.Fatal("an offset that is not there is nothing")
	}
	collection.OffsetUnset(3)
	if collection.Count() != 0 {
		t.Fatal("unsetting what is not there changed the count")
	}

	callback := NewDeferredCallback(func() {}, "one", false)
	collection.OffsetSet(-1, callback)
	if collection.First() != callback || collection.OffsetGet(0) != callback {
		t.Fatal("the callback did not land at the front")
	}

	collection.OffsetUnset(0)
	if collection.Count() != 0 {
		t.Fatalf("got %d", collection.Count())
	}
}

func TestInvokeOnANilCallbackDoesNothing(t *testing.T) {
	NewDeferredCallback(nil, "empty", false).Invoke()
}
