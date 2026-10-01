package model_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/model/relations"
)

// TestARelationMissIsAModelMiss: a failing read on a relation and FindOrFail
// both mean "the model the caller needed is not there". They were two
// sentinels, so a handler checking model.ErrModelNotFound answered a missing
// related row with 500. They are one value now, matched in both directions.
func TestARelationMissIsAModelMiss(t *testing.T) {
	relationMiss := fmt.Errorf("%w: table posts, key 7", relations.ErrModelNotFound)
	if !errors.Is(relationMiss, model.ErrModelNotFound) {
		t.Fatal("a relation miss must match model.ErrModelNotFound")
	}

	modelMiss := &model.ModelNotFoundError{Model: "users", IDs: []any{7}}
	if !errors.Is(modelMiss, relations.ErrModelNotFound) {
		t.Fatal("a FindOrFail miss must match relations.ErrModelNotFound")
	}
	if !errors.Is(modelMiss, model.ErrModelNotFound) {
		t.Fatal("ModelNotFoundError must keep matching model.ErrModelNotFound")
	}
}

// TestTheModelMissKeepsItsMessage: ModelNotFoundError prefixes the sentinel's
// text, so the sentence in a log line is unchanged by the join.
func TestTheModelMissKeepsItsMessage(t *testing.T) {
	err := &model.ModelNotFoundError{Model: "users", IDs: []any{7}}
	if got, want := err.Error(), "model: no query results for model: users [7]"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
