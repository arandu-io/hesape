package concerns

import (
	"reflect"
	"strings"
	"testing"
)

// newInverse wires a SupportsInverseRelations the way a has-many wires it: a
// parent, a related model whose declared relations are what chaperone is checked
// against, and the candidate list.
func newInverse(t *testing.T, declared ...string) (*SupportsInverseRelations, *fakeModel, *fakeModel) {
	t.Helper()

	parent := newFakeModel("users")
	related := newFakeModel("posts")
	for _, name := range declared {
		related.isRelation[name] = true
	}

	inverse := &SupportsInverseRelations{
		InverseRelatedModel: func() Model { return related },
		InverseParent:       func() Model { return parent },
	}
	inverse.PossibleInverseRelations = func() []string {
		return DefaultPossibleInverseRelations("user_id", parent, related)
	}

	return inverse, parent, related
}

// TestChaperoneRefusesARelationTheRelatedModelDoesNotDeclare.
//
// The PHP throws RelationNotFoundException. Naming a relation that is not there
// would otherwise set an attribute nobody reads, and the symptom is a second
// query per row -- the one chaperone exists to avoid -- with nothing saying why.
func TestChaperoneRefusesARelationTheRelatedModelDoesNotDeclare(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")

	err := inverse.Chaperone("editor")
	if err == nil {
		t.Fatal("chaperone accepted a relation the related model does not declare")
	}
	if !strings.Contains(err.Error(), "editor") || !strings.Contains(err.Error(), "posts") {
		t.Fatalf("Chaperone: %v, and the error has to name the relation and the model", err)
	}
	if got := inverse.GetInverseRelationship(); got != "" {
		t.Fatalf("a refused chaperone set the inverse to %q", got)
	}
}

// TestChaperoneTakesTheNameItIsGiven, when the related model declares it.
func TestChaperoneTakesTheNameItIsGiven(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")

	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}
	if got := inverse.GetInverseRelationship(); got != "author" {
		t.Fatalf("GetInverseRelationship = %q, want the name given", got)
	}
}

// TestInverseIsChaperone: two names, one behaviour, which is what the PHP has.
func TestInverseIsChaperone(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")

	if err := inverse.Inverse("author"); err != nil {
		t.Fatalf("Inverse: %v", err)
	}
	if got := inverse.GetInverseRelationship(); got != "author" {
		t.Fatalf("Inverse set %q", got)
	}
}

// TestChaperoneWithNoNameGuessesFromTheCandidates, in the order the candidate
// list gives them: the foreign key without its key suffix comes first.
func TestChaperoneWithNoNameGuessesFromTheCandidates(t *testing.T) {
	inverse, _, _ := newInverse(t, "user")

	if err := inverse.Chaperone(); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}
	if got := inverse.GetInverseRelationship(); got != "user" {
		t.Fatalf("the guess landed on %q, want the foreign key without its suffix", got)
	}
}

// TestChaperoneFallsThroughTheCandidatesInOrder: "owner" is the last of the
// defaults, and it is reached only when nothing before it is declared.
func TestChaperoneFallsThroughTheCandidatesInOrder(t *testing.T) {
	inverse, _, _ := newInverse(t, "owner")

	if err := inverse.Chaperone(); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}
	if got := inverse.GetInverseRelationship(); got != "owner" {
		t.Fatalf("the guess landed on %q, want the last candidate", got)
	}
}

// TestChaperoneWithNothingToGuessFromSaysNull.
//
// The message is what a person reads when neither a name nor a guess was
// available, and "undefined relationship []" would say nothing about which of
// the two happened.
func TestChaperoneWithNothingToGuessFromSaysNull(t *testing.T) {
	inverse, _, _ := newInverse(t)

	err := inverse.Chaperone()
	if err == nil {
		t.Fatal("chaperone guessed a relation with no candidate declared")
	}
	if !strings.Contains(err.Error(), "[null]") {
		t.Fatalf("Chaperone: %v, want the message to say there was no name at all", err)
	}
}

// TestChaperoneWithNoRelatedModelDoesNotPanic: the fields are set by the
// relation that embeds this, and one that has not finished building itself must
// report rather than dereference.
func TestChaperoneWithNoRelatedModelDoesNotPanic(t *testing.T) {
	bare := &SupportsInverseRelations{}

	err := bare.Chaperone("author")
	if err == nil {
		t.Fatal("a chaperone with no related model was accepted")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Chaperone: %v, want the model named as unknown", err)
	}
}

// TestDefaultPossibleInverseRelationsBuildsTheCandidatesInOrder.
//
// The PHP's third candidate is the parent's class basename. There is no class
// name at run time here, so it is the morph alias -- the string the column
// actually holds.
func TestDefaultPossibleInverseRelationsBuildsTheCandidatesInOrder(t *testing.T) {
	parent := newFakeModel("users")
	related := newFakeModel("posts")

	got := DefaultPossibleInverseRelations("author_id", parent, related)
	want := []string{"author", "users", "owner"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the candidates are %v, want %v", got, want)
	}
}

// TestDefaultPossibleInverseRelationsAddsParentForASelfReference.
//
// A model related to its own table is a tree, and "parent" is the name that
// reads on one. It goes on the end, after the names derived from the column, so
// a tree that named its column something else still reaches it.
func TestDefaultPossibleInverseRelationsAddsParentForASelfReference(t *testing.T) {
	tree := DefaultPossibleInverseRelations("category_id", newFakeModel("categories"), newFakeModel("categories"))
	if len(tree) == 0 || tree[len(tree)-1] != "parent" {
		t.Fatalf("the candidates are %v, want parent last for a self reference", tree)
	}

	// A column already named parent_id derives "parent" as its first candidate,
	// and the append is then deduplicated away rather than tried twice.
	named := DefaultPossibleInverseRelations("parent_id", newFakeModel("categories"), newFakeModel("categories"))
	if len(named) == 0 || named[0] != "parent" {
		t.Fatalf("the candidates are %v, want parent derived from the column first", named)
	}
	if count := countOf(named, "parent"); count != 1 {
		t.Fatalf("the candidates are %v, and parent is in there %d times", named, count)
	}

	other := DefaultPossibleInverseRelations("category_id", newFakeModel("categories"), newFakeModel("posts"))
	for _, candidate := range other {
		if candidate == "parent" {
			t.Fatalf("the candidates are %v, and parent belongs to a self reference only", other)
		}
	}
}

func countOf(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

// TestDefaultPossibleInverseRelationsDropsEmptiesAndRepeats.
//
// The foreign key and the conventional foreign key are usually the same string,
// and trimming the key suffix off a column that is only the key leaves nothing.
// A duplicate candidate is a second lookup for the same answer; an empty one
// would match a relation registered under no name.
func TestDefaultPossibleInverseRelationsDropsEmptiesAndRepeats(t *testing.T) {
	parent := newFakeModel("users")
	related := newFakeModel("posts")

	got := DefaultPossibleInverseRelations("id", parent, related)

	seen := map[string]int{}
	for _, candidate := range got {
		if candidate == "" {
			t.Fatalf("the candidates are %v, and one of them is empty", got)
		}
		seen[candidate]++
		if seen[candidate] > 1 {
			t.Fatalf("the candidates are %v, and %q is in there twice", got, candidate)
		}
	}
}

// TestDefaultPossibleInverseRelationsWithNoParentAnswersNothing rather than
// dereferencing it.
func TestDefaultPossibleInverseRelationsWithNoParentAnswersNothing(t *testing.T) {
	if got := DefaultPossibleInverseRelations("user_id", nil, nil); got != nil {
		t.Fatalf("DefaultPossibleInverseRelations answered %v with no parent", got)
	}
}

// TestApplyInverseRelationToModelLinksTheChildBackToItsParent.
//
// That link is the whole of chaperone: reading post.Author after user.Posts does
// not go back to the database, because the parent is already on the child.
func TestApplyInverseRelationToModelLinksTheChildBackToItsParent(t *testing.T) {
	inverse, parent, _ := newInverse(t, "author")
	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}

	child := newFakeModel("posts")
	got := inverse.ApplyInverseRelationToModel(child)

	if got != Model(child) {
		t.Fatal("ApplyInverseRelationToModel did not answer the model it was given")
	}
	loaded, ok := child.GetRelation("author")
	if !ok {
		t.Fatal("the inverse relation was not set, so reading it goes back to the database")
	}
	if loaded != Model(parent) {
		t.Fatalf("the inverse relation holds %#v, want the parent", loaded)
	}
}

// TestApplyInverseRelationToModelPrefersTheParentItIsGiven.
//
// An eager load matches many children to many parents, and each child gets the
// parent it matched rather than the relation's own.
func TestApplyInverseRelationToModelPrefersTheParentItIsGiven(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")
	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}

	matched := newFakeModel("users")
	matched.attributes["id"] = 9

	child := newFakeModel("posts")
	inverse.ApplyInverseRelationToModel(child, matched)

	loaded, _ := child.GetRelation("author")
	if loaded != Model(matched) {
		t.Fatal("the child was linked to the relation's parent rather than the one it matched")
	}
}

// TestApplyInverseRelationDoesNothingWithoutAChaperone, which is what makes it
// safe to call on every matched row.
func TestApplyInverseRelationDoesNothingWithoutAChaperone(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")

	child := newFakeModel("posts")
	inverse.ApplyInverseRelationToModel(child)

	if len(child.relations) != 0 {
		t.Fatalf("a relation with no chaperone set %#v on the child", child.relations)
	}

	if got := inverse.ApplyInverseRelationToModel(nil); got != nil {
		t.Fatalf("ApplyInverseRelationToModel answered %#v for a nil model", got)
	}
}

// TestApplyInverseRelationToCollectionLinksEveryModel and answers the slice it
// was given.
func TestApplyInverseRelationToCollectionLinksEveryModel(t *testing.T) {
	inverse, parent, _ := newInverse(t, "author")
	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}

	children := []Model{newFakeModel("posts"), newFakeModel("posts"), newFakeModel("posts")}
	got := inverse.ApplyInverseRelationToCollection(children)

	if len(got) != len(children) {
		t.Fatalf("ApplyInverseRelationToCollection answered %d models for %d", len(got), len(children))
	}
	for i, child := range got {
		loaded, ok := child.GetRelation("author")
		if !ok {
			t.Fatalf("model %d was not linked, so reading its parent goes back to the database", i)
		}
		if loaded != Model(parent) {
			t.Fatalf("model %d holds %#v as its parent", i, loaded)
		}
	}
}

// TestWithoutChaperoneTurnsTheLinkingOff, under both names.
func TestWithoutChaperoneTurnsTheLinkingOff(t *testing.T) {
	inverse, _, _ := newInverse(t, "author")

	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}
	inverse.WithoutChaperone()
	if got := inverse.GetInverseRelationship(); got != "" {
		t.Fatalf("WithoutChaperone left %q", got)
	}

	if err := inverse.Chaperone("author"); err != nil {
		t.Fatalf("Chaperone: %v", err)
	}
	inverse.WithoutInverse()
	if got := inverse.GetInverseRelationship(); got != "" {
		t.Fatalf("WithoutInverse left %q", got)
	}
}

// TestNonEmptyAndUniqueKeepTheOrderTheyWereGiven.
//
// The candidate list is a fall-through, so the order is the answer: reordering
// it picks a different relation.
func TestNonEmptyAndUniqueKeepTheOrderTheyWereGiven(t *testing.T) {
	got := nonEmpty([]string{"a", "", "b", "", "c"})
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("nonEmpty = %v", got)
	}

	got = unique([]string{"a", "b", "a", "c", "b"})
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("unique = %v, want the first of each in the order given", got)
	}
}
