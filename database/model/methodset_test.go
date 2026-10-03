package model_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/database/model"
)

// Every method of Model is promoted onto every entity, so its method set is the
// part of the API an application meets on its own struct -- user.Save,
// user.IsDirty -- and a name on it is a name no entity can use for its own
// method without hiding the model's.

// TestModelHasNoMethodThatChangesHowAnEntityEncodesOrPrints: a method with one
// of these names would be promoted onto every entity and silently replace how it
// is marshalled, printed, logged, scanned or stored -- a User whose json is the
// model's and not its fields, a User that is an error.
func TestModelHasNoMethodThatChangesHowAnEntityEncodesOrPrints(t *testing.T) {
	forbidden := []string{
		"MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText",
		"String", "Format", "GoString", "LogValue",
		"Scan", "Value", "GobEncode", "GobDecode",
		"MarshalBinary", "UnmarshalBinary", "Error",
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[model.Model](), reflect.TypeFor[*model.Model]()} {
		for i := range typ.NumMethod() {
			if name := typ.Method(i).Name; slices.Contains(forbidden, name) {
				t.Errorf("%s has a method %s, which every entity would inherit", typ, name)
			}
		}
	}
}

// TestModelPromotesTheMethodsItDocuments: the promoted surface is a decision,
// and this is where it is written down. A method added to Model is a method on
// every entity of every application, so adding one means adding it here.
func TestModelPromotesTheMethodsItDocuments(t *testing.T) {
	want := []string{
		// Persistence.
		"Delete", "DeleteOrFail", "DeleteQuietly", "ForceDelete", "Push",
		"Refresh", "Restore", "Save", "SaveOrFail", "SaveQuietly", "Touch", "Update",
		// State.
		"Exists", "GetChanges", "GetDirty", "GetOriginal", "IsClean", "IsDirty",
		"SyncOriginal", "Trashed", "WasChanged", "WasRecentlyCreated",
		// Attributes.
		"Fill", "ForceFill", "GetAttribute", "GetAttributes", "GetKey",
		"MakeHidden", "MakeVisible", "SetAttribute", "SetRawAttributes", "ToArray", "ToJSON",
		// Relations.
		"Load", "LoadCount", "LoadMissing", "Related", "RelationLoaded", "SetRelation",
		// Other.
		"Fresh", "Is", "Replicate", "Table", "WithoutEvents",
	}
	slices.Sort(want)

	typ := reflect.TypeFor[*model.Model]()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Model promotes\n%v\nwant\n%v", got, want)
	}
}
