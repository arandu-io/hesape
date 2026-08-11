package queue

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// ModelIdentifier is a record reduced to what finds it again.
//
// It answers Illuminate\Contracts\Database\ModelIdentifier: the value
// SerializesModels puts on the wire in place of a model. Class is what the
// application registered a finder under, ID is the primary key, and Relations
// are the relations to load back with it.
type ModelIdentifier struct {
	// Class names the kind of record. It is a string a finder is registered
	// under -- "invoice", "user" -- where PHP puts the class name, because Go
	// has no way to turn a name back into a type.
	Class string
	// ID is the primary key.
	ID string
	// Relations are the relations to load with it. Empty means none, which is
	// the right default: a job that needs a relation says so, and one that does
	// not should not pay for it.
	Relations []string
	// Connection names the database connection it came from, and empty means
	// the default.
	Connection string
	// TenantID is who it belongs to. It is not in Laravel's identifier, and it
	// is the thing that makes restoring one safe: the finder is given a Grant
	// built for this tenant, so a payload that names another customer's row
	// finds nothing (RULE 14).
	TenantID string
}

// ModelFinder loads a record back from its identifier.
//
// It is what the application registers so a job can carry an id instead of a
// document. It takes the Grant the job runs under, which is what makes the
// reload obey the same policy the original read did (RULE 17).
type ModelFinder func(ctx context.Context, g auth.Grant, id ModelIdentifier) (any, error)

// ErrMissingModel is returned when a record a job's payload names is gone.
//
// A job whose record was deleted cannot succeed on any retry. A handler that
// wants that treated as success rather than as failure says so with
// attributes.Attributes.DeleteWhenMissingModels.
var ErrMissingModel = fmt.Errorf("queue: the record this job is about no longer exists")

// SerializesModels keeps records out of job payloads.
//
// It answers Illuminate\Queue\SerializesModels and its half,
// SerializesAndRestoresModelIdentifiers. In PHP the trait hooks __sleep and
// __wakeup so a job class that holds an Eloquent model puts a ModelIdentifier
// on the wire and gets the model back on the other side, reloaded from the
// database.
//
// Go has no __sleep, so the mechanism cannot be automatic and the rule it
// enforces still is: a payload carries ids and facts, never a document. This
// type is that rule with a name and a finder registry --
// [SerializesModels.RestoreModel] is the reload, and it is the method the PHP
// trait is named for.
//
//	var models queue.SerializesModels
//	models.FindModelsUsing("invoice", func(ctx context.Context, g auth.Grant, id queue.ModelIdentifier) (any, error) {
//		return invoices.Find(ctx, g, id.ID)
//	})
//
// Why it matters is not size. A job that carries a serialized record acts on
// the record as it was when the job was queued, and the queue exists precisely
// because time passes between those two moments: the invoice was voided, the
// address was corrected, the user was deleted. Reloading is what makes the job
// act on what is true now.
type SerializesModels struct {
	finders map[string]ModelFinder
}

// FindModelsUsing registers how to load a kind of record back.
//
// It has no PHP name because PHP needs none: there the class name in the
// payload is enough to resolve the model out of the container. Here the name is
// a string and this is what turns it into code, which is the same trade the
// [Worker] makes for job names (ADR 0001).
func (s *SerializesModels) FindModelsUsing(class string, find ModelFinder) *SerializesModels {
	if s.finders == nil {
		s.finders = map[string]ModelFinder{}
	}
	s.finders[class] = find
	return s
}

// RestoreModel loads the record an identifier names.
//
// It answers restoreModel(). The Grant is rebuilt from the identifier's tenant,
// so the reload is scoped to the customer the job belongs to and a payload
// naming another customer's row finds nothing (RULE 14, RULE 17).
//
// A record that is gone comes back as an error wrapping [ErrMissingModel], not
// as a nil the handler has to remember to check.
func (s *SerializesModels) RestoreModel(ctx context.Context, action auth.Action, id ModelIdentifier) (any, error) {
	find, registered := s.finders[id.Class]
	if !registered {
		return nil, fmt.Errorf("queue: nothing knows how to load a %q. Register it with FindModelsUsing in bootstrap/app.go", id.Class)
	}
	if id.TenantID == "" {
		return nil, fmt.Errorf("queue: the identifier for %s carries no tenant, and a record cannot be loaded without one", id.Class)
	}

	model, err := find(ctx, auth.SystemGrant(action, id.TenantID), id)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, fmt.Errorf("%w: %s %s", ErrMissingModel, id.Class, id.ID)
	}
	return model, nil
}

// GetSerializedPropertyValue is what goes on the wire in place of a record.
//
// It answers getSerializedPropertyValue() from
// SerializesAndRestoresModelIdentifiers. A value that can identify itself is
// reduced to its identifier; everything else is passed through, because a
// payload of plain facts is already what it should be.
func (s *SerializesModels) GetSerializedPropertyValue(value any) any {
	if model, can := value.(Identifiable); can {
		return model.ModelIdentifier()
	}
	return value
}

// GetRestoredPropertyValue is the record an identifier names, or the value
// itself when it is not one.
//
// It answers getRestoredPropertyValue().
func (s *SerializesModels) GetRestoredPropertyValue(ctx context.Context, action auth.Action, value any) (any, error) {
	if id, is := value.(ModelIdentifier); is {
		return s.RestoreModel(ctx, action, id)
	}
	return value, nil
}

// Identifiable is a record that can say what finds it again.
//
// It is what a domain type implements so [SerializesModels] can reduce it. The
// name is new: in PHP the check is `$value instanceof Model`, and there is no
// Model here to check against (ADR 0001 rejected Active Record).
type Identifiable interface {
	// ModelIdentifier is what finds this record again.
	ModelIdentifier() ModelIdentifier
}
