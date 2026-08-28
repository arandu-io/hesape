package concerns

import "testing"

// newDefaults wires a SupportsDefaultModels the way a has-one wires it: the
// abstract method that builds a fresh related instance arrives as a field.
func newDefaults() (*SupportsDefaultModels, *int) {
	var built int
	defaults := &SupportsDefaultModels{}
	defaults.NewRelatedInstanceFor = func(Model) Model {
		built++
		return newFakeModel("profiles")
	}
	return defaults, &built
}

// TestGetDefaultForAnswersNothingUntilItIsAskedFor.
//
// nil is what makes the caller's `first() or getDefaultFor()` read the same as
// the PHP's. A relation that answered an empty model without being asked would
// turn every missing row into one that looks present.
func TestGetDefaultForAnswersNothingUntilItIsAskedFor(t *testing.T) {
	defaults, built := newDefaults()
	parent := newFakeModel("users")

	if got := defaults.GetDefaultFor(parent); got != nil {
		t.Fatalf("GetDefaultFor = %#v before withDefault was called", got)
	}
	if *built != 0 {
		t.Fatalf("a default instance was built %d times without being asked for", *built)
	}
}

// TestWithDefaultBuildsAnEmptyInstance.
func TestWithDefaultBuildsAnEmptyInstance(t *testing.T) {
	defaults, built := newDefaults()
	parent := newFakeModel("users")

	if got := defaults.WithDefault(); got != defaults {
		t.Fatal("WithDefault did not answer the receiver, so it cannot be chained")
	}

	instance := defaults.GetDefaultFor(parent)
	if instance == nil {
		t.Fatal("GetDefaultFor answered nothing after withDefault")
	}
	if *built != 1 {
		t.Fatalf("the instance was built %d times", *built)
	}
	if len(instance.GetAttributes()) != 0 {
		t.Fatalf("the default carries %#v, and withDefault with no argument fills nothing", instance.GetAttributes())
	}
}

// TestWithDefaultAttributesFillsTheInstance.
func TestWithDefaultAttributesFillsTheInstance(t *testing.T) {
	defaults, _ := newDefaults()
	parent := newFakeModel("users")

	defaults.WithDefaultAttributes(map[string]any{"name": "Anonymous", "tier": 0})

	instance := defaults.GetDefaultFor(parent)
	if instance == nil {
		t.Fatal("GetDefaultFor answered nothing")
	}
	if instance.GetAttribute("name") != "Anonymous" || instance.GetAttribute("tier") != 0 {
		t.Fatalf("the default carries %#v", instance.GetAttributes())
	}
}

// TestWithDefaultCallbackIsHandedTheInstanceAndTheParent.
//
// The parent is what makes the callback worth having: a default named after the
// user it belongs to cannot be written as a fixed attribute map.
func TestWithDefaultCallbackIsHandedTheInstanceAndTheParent(t *testing.T) {
	defaults, _ := newDefaults()
	parent := newFakeModel("users")
	parent.attributes["name"] = "Ada"

	var gotParent Model
	var calls int
	defaults.WithDefaultCallback(func(instance, owner Model) {
		calls++
		gotParent = owner
		instance.SetAttribute("name", owner.GetAttribute("name"))
	})

	instance := defaults.GetDefaultFor(parent)
	if calls != 1 {
		t.Fatalf("the callback ran %d times", calls)
	}
	if gotParent != Model(parent) {
		t.Fatal("the callback was handed a parent that is not the one asked about")
	}
	if instance.GetAttribute("name") != "Ada" {
		t.Fatalf("the callback's writes did not land: %#v", instance.GetAttributes())
	}
}

// TestTheThreeFormsReplaceEachOther.
//
// The PHP spells all three withDefault and reads the argument's type at call
// time. Split into three methods, each has to clear what the others set, or a
// callback would still run after the caller replaced it with attributes.
func TestTheThreeFormsReplaceEachOther(t *testing.T) {
	defaults, _ := newDefaults()
	parent := newFakeModel("users")

	var callbackRan int
	defaults.WithDefaultCallback(func(Model, Model) { callbackRan++ })
	defaults.WithDefaultAttributes(map[string]any{"name": "Anonymous"})

	instance := defaults.GetDefaultFor(parent)
	if callbackRan != 0 {
		t.Fatal("the replaced callback still ran")
	}
	if instance.GetAttribute("name") != "Anonymous" {
		t.Fatalf("the attributes did not land: %#v", instance.GetAttributes())
	}

	defaults.WithDefaultCallback(func(i, _ Model) { i.SetAttribute("tier", 1) })
	instance = defaults.GetDefaultFor(parent)
	if _, filled := instance.GetAttributes()["name"]; filled {
		t.Fatalf("the replaced attributes still landed: %#v", instance.GetAttributes())
	}
	if instance.GetAttribute("tier") != 1 {
		t.Fatalf("the callback did not run: %#v", instance.GetAttributes())
	}

	defaults.WithDefault()
	instance = defaults.GetDefaultFor(parent)
	if len(instance.GetAttributes()) != 0 {
		t.Fatalf("withDefault with no argument kept %#v", instance.GetAttributes())
	}
}

// TestWithoutDefaultTurnsItBackOff, which is the PHP's withDefault(false).
func TestWithoutDefaultTurnsItBackOff(t *testing.T) {
	defaults, _ := newDefaults()
	parent := newFakeModel("users")

	defaults.WithDefaultAttributes(map[string]any{"name": "Anonymous"})
	if defaults.GetDefaultFor(parent) == nil {
		t.Fatal("the default was not on to begin with")
	}

	if got := defaults.WithoutDefault(); got != defaults {
		t.Fatal("WithoutDefault did not answer the receiver")
	}
	if got := defaults.GetDefaultFor(parent); got != nil {
		t.Fatalf("GetDefaultFor = %#v after WithoutDefault", got)
	}
}

// TestGetDefaultForAnswersNothingWhenTheRelationCannotBuildOne.
//
// The field is set by the relation that embeds this. One that has not finished
// building itself, or whose constructor answered nothing, must report the
// absence rather than dereference it.
func TestGetDefaultForAnswersNothingWhenTheRelationCannotBuildOne(t *testing.T) {
	parent := newFakeModel("users")

	unwired := &SupportsDefaultModels{}
	unwired.WithDefault()
	if got := unwired.GetDefaultFor(parent); got != nil {
		t.Fatalf("GetDefaultFor = %#v with no constructor wired", got)
	}

	empty := &SupportsDefaultModels{NewRelatedInstanceFor: func(Model) Model { return nil }}
	empty.WithDefaultAttributes(map[string]any{"name": "Anonymous"})
	if got := empty.GetDefaultFor(parent); got != nil {
		t.Fatalf("GetDefaultFor = %#v when the constructor answered nothing", got)
	}
}

// TestGetDefaultForBuildsAFreshInstanceEveryTime.
//
// Two parents with no related row each get their own default. One shared
// instance would mean a write through one parent's default showing up on the
// other's.
func TestGetDefaultForBuildsAFreshInstanceEveryTime(t *testing.T) {
	defaults, built := newDefaults()
	defaults.WithDefault()

	first := defaults.GetDefaultFor(newFakeModel("users"))
	second := defaults.GetDefaultFor(newFakeModel("users"))

	if first == second {
		t.Fatal("two parents were handed the same default instance")
	}
	if *built != 2 {
		t.Fatalf("the constructor ran %d times for two parents", *built)
	}
}
