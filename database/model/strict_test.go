package model

import (
	"errors"
	"reflect"
	"testing"
)

// resetStrict puts the process-wide switches back however the test ends.
// They are package state, so a test that left one on would decide the next
// one.
func resetStrict(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		PreventLazyLoading(false)
		PreventSilentlyDiscardingAttributes(false)
		PreventAccessingMissingAttributes(false)
		HandleLazyLoadingViolationUsing(nil)
		HandleDiscardedAttributeViolationUsing(nil)
		HandleMissingAttributeViolationUsing(nil)
	})
}

func TestShouldBeStrictTurnsOnAllThreeSwitches(t *testing.T) {
	resetStrict(t)

	ShouldBeStrict()

	if !PreventsLazyLoading() || !PreventsSilentlyDiscardingAttributes() || !PreventsAccessingMissingAttributes() {
		t.Error("shouldBeStrict must turn on all three, which is the whole of its body")
	}

	ShouldBeStrict(false)

	if PreventsLazyLoading() || PreventsSilentlyDiscardingAttributes() || PreventsAccessingMissingAttributes() {
		t.Error("shouldBeStrict(false) must turn all three off: the argument is passed through")
	}
}

func TestFillRefusesADiscardedAttributeWhenPreventing(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()

	if err := model.Fill(map[string]any{"nickname": "the countess"}); err != nil {
		t.Fatalf("Fill with the switch off must drop the key silently: %v", err)
	}

	PreventSilentlyDiscardingAttributes()

	err := model.Fill(map[string]any{"name": "Ada", "nickname": "the countess"})
	if !errors.Is(err, ErrMassAssignment) {
		t.Fatalf("Fill error = %v, want ErrMassAssignment: the PHP throws MassAssignmentException here", err)
	}
}

func TestDiscardedAttributeCallbackReplacesTheError(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	PreventSilentlyDiscardingAttributes()

	var seen []string
	HandleDiscardedAttributeViolationUsing(func(_ any, keys []string) { seen = keys })

	if err := model.Fill(map[string]any{"nickname": "the countess"}); err != nil {
		t.Fatalf("a registered callback replaces the throw, so Fill succeeds: %v", err)
	}
	if !reflect.DeepEqual(seen, []string{"nickname"}) {
		t.Errorf("callback saw %v, want [nickname]", seen)
	}
}

func TestForceFillIsNotADiscardedAttribute(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	PreventSilentlyDiscardingAttributes()

	if err := model.ForceFill(map[string]any{"posts_count": int64(3)}); err != nil {
		t.Fatalf("ForceFill keeps the key rather than discarding it, so there is no violation: %v", err)
	}
}

func TestMissingAttributeViolationReachesTheCallback(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	model.Exists = true

	var seen string
	HandleMissingAttributeViolationUsing(func(_ any, key string) { seen = key })

	if got := model.GetAttribute("nickname"); got != nil {
		t.Errorf("GetAttribute = %v, want nil: the read still answers nil", got)
	}
	if seen != "" {
		t.Error("the switch is off, so nothing is reported")
	}

	PreventAccessingMissingAttributes()
	model.GetAttribute("nickname")

	if seen != "nickname" {
		t.Errorf("callback saw %q, want nickname", seen)
	}
}

func TestMissingAttributeViolationSkipsAModelThatDoesNotExist(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	PreventAccessingMissingAttributes()

	reported := false
	HandleMissingAttributeViolationUsing(func(any, string) { reported = true })

	model.GetAttribute("nickname")

	if reported {
		t.Error("a model that was never retrieved has no attribute to be missing, as in getAttribute")
	}
}

func TestLazyLoadingViolationIsReportedForADeclaredRelation(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	model.Exists = true
	model.RelationResolvers = map[string]func(*Model[user]) Relation{"posts": nil}

	var seen string
	HandleLazyLoadingViolationUsing(func(_ any, key string) { seen = key })
	PreventLazyLoading()

	if _, ok := model.GetRelation("posts"); ok {
		t.Fatal("posts was never loaded, so GetRelation must answer false rather than run a query")
	}
	if seen != "posts" {
		t.Errorf("callback saw %q, want posts", seen)
	}
}

func TestLazyLoadingViolationIgnoresANameThatIsNotARelation(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	model.Exists = true
	PreventLazyLoading()

	reported := false
	HandleLazyLoadingViolationUsing(func(any, string) { reported = true })

	model.GetRelation("posts")

	if reported {
		t.Error("no resolver is registered for posts, so there is no relation to have loaded")
	}
}

func TestLoadedRelationIsNoViolation(t *testing.T) {
	resetStrict(t)
	model, _ := newUserModel()
	model.Exists = true
	model.RelationResolvers = map[string]func(*Model[user]) Relation{"posts": nil}
	model.SetRelation("posts", Collection[user]{})
	PreventLazyLoading()

	reported := false
	HandleLazyLoadingViolationUsing(func(any, string) { reported = true })

	if _, ok := model.GetRelation("posts"); !ok {
		t.Fatal("posts is loaded")
	}
	if reported {
		t.Error("a loaded relation is what the switch is there to require")
	}
}

func TestWithoutTouchingIgnoresTheTypeForTheCallbackOnly(t *testing.T) {
	model, _ := newUserModel()

	if model.IsIgnoringTouch() {
		t.Fatal("nothing is ignored before withoutTouching runs")
	}

	err := WithoutTouching[user](func() error {
		if !model.IsIgnoringTouch() {
			t.Error("the type is ignored for the length of the callback")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithoutTouching: %v", err)
	}

	if model.IsIgnoringTouch() {
		t.Error("the finally block puts the list back, however the callback ends")
	}
}

func TestWithoutTouchingRestoresAfterAFailure(t *testing.T) {
	model, _ := newUserModel()
	boom := errors.New("boom")

	if err := WithoutTouching[user](func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("WithoutTouching must return what the callback returned, got %v", err)
	}
	if model.IsIgnoringTouch() {
		t.Error("array_diff runs in a finally, so an error still puts the type back")
	}
}

func TestIsIgnoringTouchIsTrueWithoutTimestamps(t *testing.T) {
	model, _ := newUserModel()
	model.Timestamps = false

	if !model.IsIgnoringTouch() {
		t.Error("a model with timestamps off has no updated_at to bump, which the PHP checks before the list")
	}

	model.Timestamps = true
	model.UpdatedAtColumn = ""

	if !model.IsIgnoringTouch() {
		t.Error("UPDATED_AT of null is the PHP's first check")
	}
}
