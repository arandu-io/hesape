package model

import (
	"context"
	"strings"
	"testing"
)

func TestGetMorphClassIsTheEntityType(t *testing.T) {
	model, _ := newUserModel()

	if got := model.GetMorphClass(); got != "user" {
		t.Errorf("GetMorphClass = %q, want user: get_class before the morph map", got)
	}
}

func TestLoadMorphLoadsWhatTheTargetsClassAsksFor(t *testing.T) {
	model, _ := newUserModel()

	target, targetConn := newUserModel()
	target.Table = "posts"
	target.Exists = true
	targetConn.queue()
	model.SetRelation("commentable", target)

	err := model.LoadMorph(context.Background(), grant(), "commentable", map[string][]string{"user": {"comments"}})
	if err == nil {
		t.Fatal("the relation has no resolver behind it, so the eager load must report that rather than pass silently")
	}
	if !strings.Contains(err.Error(), "comments") {
		t.Errorf("error = %v, want it to name the relation it was asked to load", err)
	}
}

func TestLoadMorphIsAnEmptyLoadForAClassWithNoEntry(t *testing.T) {
	model, _ := newUserModel()

	target, _ := newUserModel()
	target.Exists = true
	model.SetRelation("commentable", target)

	if err := model.LoadMorph(context.Background(), grant(), "commentable", map[string][]string{"post": {"comments"}}); err != nil {
		t.Fatalf("a morph class with no entry loads nothing, as $relations[$className] ?? [] does: %v", err)
	}
}

func TestLoadMorphSkipsARelationThatPointsAtNothing(t *testing.T) {
	model, _ := newUserModel()

	if err := model.LoadMorph(context.Background(), grant(), "commentable", map[string][]string{"user": {"comments"}}); err != nil {
		t.Fatalf("the PHP returns $this when the relation is falsy: %v", err)
	}
}

func TestCollectionLoadMorphWalksEveryModel(t *testing.T) {
	first, _ := newAccountModel()
	second, _ := newAccountModel()

	target, _ := newAccountModel()
	target.Exists = true
	first.SetRelation("commentable", target)
	second.SetRelation("commentable", target)

	if err := (Collection[account]{first.Entity, second.Entity}).LoadMorph(context.Background(), grant(), "commentable", nil); err != nil {
		t.Fatalf("LoadMorph: %v", err)
	}
}
