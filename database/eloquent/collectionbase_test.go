package eloquent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func twoUsers() Collection[user] {
	first, _ := newUserModel()
	first.Entity.ID = 1
	first.Entity.Name = "Ada"
	second, _ := newUserModel()
	second.Entity.ID = 2
	second.Entity.Name = "Grace"
	return Collection[user]{first, second}
}

func TestNewCollectionWrapsTheModels(t *testing.T) {
	model, _ := newUserModel()

	if got := model.NewCollection(model); len(got) != 1 || got[0] != model {
		t.Errorf("NewCollection = %v, want the one model back", got)
	}
	if got := model.NewCollection(); len(got) != 0 {
		t.Errorf("NewCollection() = %v, want the empty collection, which is the PHP's default argument", got)
	}
}

func TestMapAnswersTheCallbacksType(t *testing.T) {
	names := Map(twoUsers(), func(m *Model[user], _ int) string { return m.Entity.Name })

	if !reflect.DeepEqual([]string(names), []string{"Ada", "Grace"}) {
		t.Errorf("Map = %v, want [Ada Grace]: the PHP drops to a base collection when the callback stopped returning models", names)
	}
}

func TestMapWithKeysPairsTheResult(t *testing.T) {
	byName := MapWithKeys(twoUsers(), func(m *Model[user], _ int) (string, int64) {
		return m.Entity.Name, m.Entity.ID
	})

	if byName["Grace"] != 2 {
		t.Errorf("MapWithKeys = %v, want Grace at 2", byName)
	}
}

func TestCountByGroupsWithTheCallersKey(t *testing.T) {
	counts := CountBy(twoUsers(), func(m *Model[user], _ int) bool { return m.Entity.ID > 1 })

	if counts[true] != 1 || counts[false] != 1 {
		t.Errorf("CountBy = %v, want one on each side", counts)
	}
}

func TestFlipMapsModelsToTheirPositions(t *testing.T) {
	c := twoUsers()

	if got := c.Flip()[c[1]]; got != 1 {
		t.Errorf("Flip put the second model at %d, want 1", got)
	}
}

func TestFlattenKeepsTheModelsAsLeaves(t *testing.T) {
	if got := twoUsers().Flatten(); len(got) != 2 {
		t.Errorf("Flatten = %v, want the two models: a model is not a list, so it is a leaf", got)
	}
}

func TestPadAndPartitionDropToTheBaseCollection(t *testing.T) {
	c := twoUsers()

	if got := c.Pad(4, nil); len(got) != 4 {
		t.Errorf("Pad gave %d models, want 4", len(got))
	}

	passed, failed := c.Partition(func(m *Model[user], _ int) bool { return m.Entity.ID == 1 })
	if len(passed) != 1 || len(failed) != 1 {
		t.Errorf("Partition = %d passed and %d failed, want one each", len(passed), len(failed))
	}
}

func TestZipPairsTheCollections(t *testing.T) {
	c := twoUsers()
	other, _ := newUserModel()

	tuples := Zip(c, []*Model[user]{other})
	if len(tuples) != 2 || len(tuples[0]) != 2 {
		t.Fatalf("Zip = %v, want two tuples of two", tuples)
	}
	if tuples[1][1] != nil {
		t.Error("the short list pads with the zero value, which is what array_map does with null there")
	}
}

func TestModelNotFoundErrorCarriesTheIDs(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	_, err := model.NewQuery().FindOrFail(context.Background(), grant(), 7)
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
