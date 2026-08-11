package concerns_test

import (
	"testing"

	"github.com/arandu-io/hesape/database/eloquent/concerns"
)

func TestDirtyTrackingComparesAgainstWhatWasLoaded(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.SetRawAttributes(map[string]any{"title": "a", "views": 1}, true)

	if attributes.IsDirty() {
		t.Fatal("a model straight out of the database is dirty")
	}

	attributes.SetAttribute("title", "b")

	if !attributes.IsDirty("title") {
		t.Error("the changed attribute is not dirty")
	}
	if attributes.IsDirty("views") {
		t.Error("an untouched attribute came back dirty")
	}

	attributes.SyncChanges()
	if !attributes.WasChanged("title") {
		t.Error("the change log did not record the change")
	}
	if attributes.GetPrevious()["title"] != "a" {
		t.Errorf("the previous value was %v, want the one that was loaded", attributes.GetPrevious()["title"])
	}
}

// A column that came back as the string "1" and is set to the integer 1 is not
// a change. PHP compares loosely and answers the same; a strict comparison here
// would write an UPDATE on every save of a row that nobody edited.
func TestAnAttributeSetToTheSameValueInAnotherTypeIsNotDirty(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.SetRawAttributes(map[string]any{"views": "1"}, true)

	attributes.SetAttribute("views", 1)

	if attributes.IsDirty("views") {
		t.Error(`"1" and 1 are the same column value, and an update that rewrites it is a write nobody asked for`)
	}
}

func TestDiscardChangesGoesBackToWhatWasLoaded(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.SetRawAttributes(map[string]any{"title": "a"}, true)

	attributes.SetAttribute("title", "b")
	attributes.DiscardChanges()

	if attributes.GetAttribute("title") != "a" {
		t.Errorf("title is %v after discarding, want the loaded value", attributes.GetAttribute("title"))
	}
}

func TestAModelIsTotallyGuardedByDefault(t *testing.T) {
	var guard concerns.GuardsAttributes

	if !guard.TotallyGuarded() {
		t.Fatal("a model that declares nothing accepted mass assignment: the default guard is [*], and refusing is the safe direction")
	}
	if guard.IsFillable("is_admin") {
		t.Error("is_admin was fillable on a model with no fillable list")
	}

	guard.Fillable([]string{"title"})
	if !guard.IsFillable("title") || guard.IsFillable("is_admin") {
		t.Error("the fillable list did not narrow mass assignment to itself")
	}

	filled := guard.FillableFromArray(map[string]any{"title": "a", "is_admin": true})
	if _, ok := filled["is_admin"]; ok {
		t.Error("a guarded attribute survived fillableFromArray")
	}
}

func TestUnguardedPutsTheGuardBackEvenWhenTheCallbackFails(t *testing.T) {
	err := concerns.Unguarded(func() error {
		if !concerns.IsUnguarded() {
			t.Error("the guard was still on inside unguarded")
		}
		return errFailed
	})

	if err != errFailed {
		t.Fatalf("unguarded swallowed the error: %v", err)
	}
	if concerns.IsUnguarded() {
		t.Fatal("the guard stayed off after the callback failed")
	}
}

var errFailed = errString("the callback failed")

type errString string

func (e errString) Error() string { return string(e) }

func TestHiddenAndVisibleNarrowTheSerializedModel(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.SetRawAttributes(map[string]any{"id": "1", "email": "a@b.c", "password": "secret"}, true)

	var hides concerns.HidesAttributes
	hides.SetHidden([]string{"password"})

	out := attributes.AttributesToArray(&hides)
	if _, ok := out["password"]; ok {
		t.Error("a hidden attribute was serialized")
	}
	if out["email"] != "a@b.c" {
		t.Error("a visible attribute was dropped")
	}

	hides.MakeVisible("password")
	if _, ok := attributes.AttributesToArray(&hides)["password"]; !ok {
		t.Error("makeVisible did not undo the hiding")
	}

	hides.SetVisible([]string{"id"})
	out = attributes.AttributesToArray(&hides)
	if len(out) != 1 {
		t.Errorf("a visible list of one produced %d attributes, want an allowlist", len(out))
	}
}

func TestTimestampsAreStampedOnInsertAndOnlyUpdatedOnUpdate(t *testing.T) {
	var timestamps concerns.HasTimestamps

	inserting := map[string]any{}
	timestamps.UpdateTimestamps(inserting, false)
	if _, ok := inserting[concerns.CreatedAt]; !ok {
		t.Error("created_at was not stamped on an insert")
	}
	if _, ok := inserting[concerns.UpdatedAt]; !ok {
		t.Error("updated_at was not stamped on an insert")
	}

	updating := map[string]any{}
	timestamps.UpdateTimestamps(updating, true)
	if _, ok := updating[concerns.CreatedAt]; ok {
		t.Error("created_at was rewritten on an update: the row would claim to have been created twice")
	}
}

func TestWithoutTimestampsSuspendsThemAndPutsThemBack(t *testing.T) {
	var timestamps concerns.HasTimestamps

	err := concerns.WithoutTimestamps(func() error {
		attributes := map[string]any{}
		timestamps.UpdateTimestamps(attributes, false)
		if len(attributes) != 0 {
			t.Error("a timestamp was written inside withoutTimestamps")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !timestamps.UsesTimestamps() {
		t.Fatal("timestamps stayed off after the callback returned")
	}
}

func TestUniqueIdsFillOnlyTheEmptyColumns(t *testing.T) {
	ulids := concerns.HasULIDs()

	attributes := map[string]any{}
	ulids.SetUniqueIDs(attributes, "id")

	key, _ := attributes["id"].(string)
	if !concerns.IsValidUniqueID(key) {
		t.Fatalf("the generated key was %q, want a ULID", key)
	}

	attributes = map[string]any{"id": "chosen-by-hand"}
	ulids.SetUniqueIDs(attributes, "id")
	if attributes["id"] != "chosen-by-hand" {
		t.Error("a key somebody set was overwritten")
	}
}

func TestWithoutRecursionAnswersTheDefaultOnTheSecondEntry(t *testing.T) {
	var guard concerns.PreventsCircularRecursion

	depth := 0
	var walk func() any
	walk = func() any {
		return guard.WithoutRecursion("toArray", func() any {
			depth++
			if depth > 5 {
				t.Fatal("the recursion was not stopped")
			}
			return walk()
		}, "stopped")
	}

	if got := walk(); got != "stopped" {
		t.Fatalf("the second entry returned %v, want the default", got)
	}
	if guard.IsRecursing("toArray") {
		t.Error("the call was left on the stack after it returned")
	}
}

func TestModelEventsCanVetoAnOperation(t *testing.T) {
	var events concerns.HasEvents

	events.Saving(func(any) bool { return false })
	events.Saving(func(any) bool {
		t.Error("a listener ran after one had already halted the operation")
		return true
	})

	if events.FireModelEvent(concerns.Saving, nil, true) {
		t.Fatal("a listener returning false did not halt the save")
	}
}

func TestWithoutEventsMutesTheListeners(t *testing.T) {
	var events concerns.HasEvents
	events.Saving(func(any) bool { return false })

	err := concerns.WithoutEvents(func() error {
		if !events.FireModelEvent(concerns.Saving, nil, true) {
			t.Error("a muted listener still halted the operation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if events.FireModelEvent(concerns.Saving, nil, true) {
		t.Fatal("the listeners stayed muted after the callback returned")
	}
}
