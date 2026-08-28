package concerns

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// newCompares wires a ComparesRelatedModels the way a has-one wires it: the
// related model, the parent key, and how to read the matching key off a
// candidate.
func newCompares(parentKey any, related *fakeModel) ComparesRelatedModels {
	return ComparesRelatedModels{
		CompareRelated:        related,
		CompareParentKey:      func() any { return parentKey },
		CompareRelatedKeyFrom: func(m Model) any { return m.GetAttribute("user_id") },
	}
}

// TestIsMatchesOnTheKeyAndTheTable.
//
// Two models of different tables can hold the same key, and a relation that
// compared only the key would say a post is the comment that shares its id.
func TestIsMatchesOnTheKeyAndTheTable(t *testing.T) {
	related := newFakeModel("posts")
	compares := newCompares(int64(7), related)

	same := newFakeModel("posts")
	same.attributes["user_id"] = int64(7)

	is, err := compares.Is(context.Background(), grant(), same)
	if err != nil {
		t.Fatalf("Is: %v", err)
	}
	if !is {
		t.Fatal("a model of the same table holding the same key was not recognised")
	}

	elsewhere := newFakeModel("comments")
	elsewhere.attributes["user_id"] = int64(7)

	is, err = compares.Is(context.Background(), grant(), elsewhere)
	if err != nil {
		t.Fatalf("Is: %v", err)
	}
	if is {
		t.Fatal("a model of another table was matched on its key alone")
	}
}

// TestIsAndIsNotAreOpposites, including on the error path: a caller that reads
// only IsNot must not be told "not this one" because a query failed.
func TestIsAndIsNotAreOpposites(t *testing.T) {
	related := newFakeModel("posts")
	compares := newCompares(int64(7), related)

	match := newFakeModel("posts")
	match.attributes["user_id"] = int64(7)

	is, err := compares.Is(context.Background(), grant(), match)
	if err != nil {
		t.Fatalf("Is: %v", err)
	}
	isNot, err := compares.IsNot(context.Background(), grant(), match)
	if err != nil {
		t.Fatalf("IsNot: %v", err)
	}
	if is == isNot {
		t.Fatalf("Is and IsNot both answered %v", is)
	}

	boom := errors.New("the existence query failed")
	compares.CompareOneOfMany = func(context.Context, auth.Grant, Model) (bool, error) {
		return false, boom
	}
	if _, err := compares.IsNot(context.Background(), grant(), match); !errors.Is(err, boom) {
		t.Fatalf("IsNot answered %v, want the error the query reported", err)
	}
}

// TestIsAsksTheOneOfManyBranchWhenTheKeysAlreadyMatch.
//
// On a one-of-many relation the keys matching is not the question: the relation
// is one row of many, and whether this is that row takes a query.
func TestIsAsksTheOneOfManyBranchWhenTheKeysAlreadyMatch(t *testing.T) {
	related := newFakeModel("posts")
	compares := newCompares(int64(7), related)

	var asked int
	compares.CompareOneOfMany = func(context.Context, auth.Grant, Model) (bool, error) {
		asked++
		return false, nil
	}

	match := newFakeModel("posts")
	match.attributes["user_id"] = int64(7)

	is, err := compares.Is(context.Background(), grant(), match)
	if err != nil {
		t.Fatalf("Is: %v", err)
	}
	if asked != 1 {
		t.Fatalf("the one-of-many branch was asked %d times", asked)
	}
	if is {
		t.Fatal("the keys matched and the branch said no, and the branch is the answer")
	}
}

// TestIsDoesNotAskTheOneOfManyBranchWhenTheKeysDoNotMatch, which is what keeps
// the comparison from costing a query for every model it is handed.
func TestIsDoesNotAskTheOneOfManyBranchWhenTheKeysDoNotMatch(t *testing.T) {
	related := newFakeModel("posts")
	compares := newCompares(int64(7), related)

	var asked int
	compares.CompareOneOfMany = func(context.Context, auth.Grant, Model) (bool, error) {
		asked++
		return true, nil
	}

	other := newFakeModel("posts")
	other.attributes["user_id"] = int64(9)

	is, err := compares.Is(context.Background(), grant(), other)
	if err != nil {
		t.Fatalf("Is: %v", err)
	}
	if asked != 0 {
		t.Fatal("a model whose key does not match cost a query anyway")
	}
	if is {
		t.Fatal("a model whose key does not match was reported as the one")
	}
}

// TestIsIsFalseWithNothingToCompareAgainst, rather than dereferencing the nil.
func TestIsIsFalseWithNothingToCompareAgainst(t *testing.T) {
	related := newFakeModel("posts")

	is, err := newCompares(int64(7), related).Is(context.Background(), grant(), nil)
	if err != nil || is {
		t.Fatalf("Is(nil) = %v, %v", is, err)
	}

	bare := ComparesRelatedModels{
		CompareParentKey:      func() any { return int64(7) },
		CompareRelatedKeyFrom: func(Model) any { return int64(7) },
	}
	is, err = bare.Is(context.Background(), grant(), newFakeModel("posts"))
	if err != nil || is {
		t.Fatalf("Is with no related model = %v, %v", is, err)
	}
}

// TestCompareKeysRefusesTwoEmptyKeys.
//
// This is the first branch and the whole point of it: two unsaved models both
// have a nil key, and comparing them by value would make every unsaved model
// equal to every other.
func TestCompareKeysRefusesTwoEmptyKeys(t *testing.T) {
	for _, empty := range []any{nil, "", 0, int64(0), uint64(0), float64(0)} {
		if CompareKeys(empty, empty) {
			t.Errorf("CompareKeys(%#v, %#v) matched, and an unsaved model is not every other one", empty, empty)
		}
		if CompareKeys(empty, int64(7)) || CompareKeys(int64(7), empty) {
			t.Errorf("CompareKeys matched %#v against a real key", empty)
		}
	}
}

// TestCompareKeysCoercesTheWayAnArraySubscriptDoes.
//
// The PHP compares as integers when either side is one. Here both sides go
// through the dictionary key, which is the same coercion an array subscript
// performs -- and which is what lets a key the driver returned as text match the
// one the caller holds as a number.
func TestCompareKeysCoercesTheWayAnArraySubscriptDoes(t *testing.T) {
	for _, c := range []struct {
		left, right any
		want        bool
	}{
		{int64(7), int64(7), true},
		{int64(7), 7, true},
		{int64(7), "7", true},
		{"7", int64(7), true},
		{float64(7), int64(7), true},
		{"admin", "admin", true},
		{[]byte("admin"), "admin", true},

		{int64(7), int64(8), false},
		{"admin", "editor", false},
		{int64(7), "seven", false},
	} {
		if got := CompareKeys(c.left, c.right); got != c.want {
			t.Errorf("CompareKeys(%#v, %#v) = %v, want %v", c.left, c.right, got, c.want)
		}
	}
}

// TestCompareKeysRefusesAKeyItCannotRender rather than treating two
// unrenderable values as equal, which is the empty-key trap one level up.
func TestCompareKeysRefusesAKeyItCannotRender(t *testing.T) {
	unrenderable := struct{ A int }{A: 1}

	if CompareKeys(unrenderable, unrenderable) {
		t.Fatal("two values the dictionary cannot key were compared equal")
	}
	if CompareKeys(unrenderable, int64(7)) || CompareKeys(int64(7), unrenderable) {
		t.Fatal("a value the dictionary cannot key was matched against a real one")
	}
}

// TestIsEmptyKeyCoversWhatAKeyCanHold, and nothing else: a non-zero value of any
// of those types is a key, and so is a type not in the list.
func TestIsEmptyKeyCoversWhatAKeyCanHold(t *testing.T) {
	for _, empty := range []any{nil, "", 0, int64(0), uint64(0), float64(0)} {
		if !isEmptyKey(empty) {
			t.Errorf("isEmptyKey(%#v) = false", empty)
		}
	}
	for _, filled := range []any{"admin", 1, int64(1), uint64(1), float64(0.5), true, []byte("x")} {
		if isEmptyKey(filled) {
			t.Errorf("isEmptyKey(%#v) = true, and it is a key", filled)
		}
	}
}
