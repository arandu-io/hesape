package model

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func twoAccounts() Rows {
	first, _ := newAccountModel()
	first.ID = 1
	first.Name = "Ada"
	second, _ := newAccountModel()
	second.ID = 2
	second.Name = "Grace"
	return Rows{first, second}
}

func TestRowsAnswerTheirSizeAndTheirFirst(t *testing.T) {
	rows := twoAccounts()

	if rows.Count() != 2 || rows.IsEmpty() || !rows.IsNotEmpty() {
		t.Errorf("Count = %d, IsEmpty = %v for two rows", rows.Count(), rows.IsEmpty())
	}
	if rows.First() != rows[0] {
		t.Error("First is not the first row")
	}
	if (Rows{}).First() != nil || !(Rows{}).IsEmpty() {
		t.Error("no rows has a first row")
	}
}

func TestMergeReplacesARowWithTheSameKey(t *testing.T) {
	rows := twoAccounts()
	replacement, _ := newAccountModel()
	replacement.ID = 2
	replacement.Name = "Grace Hopper"

	merged := rows.Merge(Rows{replacement})
	if len(merged) != 2 {
		t.Fatalf("Merge = %d rows, want 2: the key was already there", len(merged))
	}
	if merged[1].(*account).Name != "Grace Hopper" {
		t.Errorf("Merge kept %q, want the row merged in", merged[1].(*account).Name)
	}
}

func TestModelNotFoundErrorCarriesTheIDs(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	_, err := newQuery(model.base()).FindOrFail(context.Background(), grant(), 7)
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("error = %v, want ErrModelNotFound", err)
	}

	var notFound *ModelNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("the ids must be reachable with errors.As, which is what getIds is for")
	}
	if notFound.GetModel() != "users" || !reflect.DeepEqual(notFound.GetIDs(), []any{7}) {
		t.Errorf("GetModel = %q and GetIDs = %v, want users and [7]", notFound.GetModel(), notFound.GetIDs())
	}
}

func TestJSONEncodingErrorsNameWhatFailed(t *testing.T) {
	err := ForModel("users", 7, "unsupported type")
	if !errors.Is(err, ErrJSONEncoding) || !strings.Contains(err.Error(), "users") {
		t.Errorf("ForModel = %v, want it wrapped and naming the model", err)
	}
	if err := ForAttribute("users", "meta", "unsupported type"); !strings.Contains(err.Error(), "meta") {
		t.Errorf("ForAttribute = %v, want it naming the attribute", err)
	}
	if err := ForResource("UserResource", "users", 7, "unsupported type"); !strings.Contains(err.Error(), "UserResource") {
		t.Errorf("ForResource = %v, want it naming the resource", err)
	}
}
