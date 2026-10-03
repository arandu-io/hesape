package model

import "testing"

func TestTheMorphClassIsTheEntityType(t *testing.T) {
	model, _ := newUserModel()

	if got := refOf(model.base()).GetMorphClass(); got != "user" {
		t.Errorf("GetMorphClass = %q, want user: get_class before the morph map", got)
	}
}

// TestMorphModelIsAnEmptyRowAsARelationTakesIt: a morph map entry answers the
// model a polymorphic relation reads the type's table through, on the
// connection the application gave it.
func TestMorphModelIsAnEmptyRowAsARelationTakesIt(t *testing.T) {
	conn := newTestConnection()
	target := newPostTable().MorphModel(conn)

	if target.GetTable() != "posts" || target.GetMorphClass() != "post" {
		t.Errorf("MorphModel = table %q, class %q; want posts and post", target.GetTable(), target.GetMorphClass())
	}
	if target.Exists() {
		t.Error("an empty row reports that it is stored")
	}
}
