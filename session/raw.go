package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Keeper is a store that can also keep sessions.
//
// A shared cache store reaches the application through a registry that knows
// nothing about the payload type, and a generic [Handler] cannot be built from
// a type named at run time. So a store that keeps sessions offers them with the
// payload still encoded, and the application asks for them where it knows its
// payload type:
//
//	if keeper, ok := store.(session.Keeper); ok {
//	    handler := session.Decode[auth.Subject](keeper.Sessions())
//	}
//
// It is an optional interface, asserted by the code that wants sessions, so a
// store that keeps none implements nothing extra.
type Keeper interface {
	// Sessions returns the handler over the store's connection, with each
	// payload as the JSON it is stored as.
	Sessions() Handler[json.RawMessage]
}

// Decode returns a [Handler] for payloads of type T over a handler that stores
// them encoded.
//
// The payload is marshalled on the way in and unmarshalled on the way out;
// Tenant, SubjectID, Remembered and PasswordConfirmedAt pass through untouched,
// because they are the index and the lifetime, and a handler that rewrote them
// would sign people out of sessions it was never asked about.
//
// A handler that marshals the whole record stores the same bytes either way.
// encoding/json compacts and escapes a json.RawMessage exactly as it encodes
// the value the message was marshalled from, so a Record[json.RawMessage]
// carrying the encoded payload serializes as the typed record it came from. A
// session written by a handler for T reads back through Decode, and the
// reverse.
func Decode[T any](h Handler[json.RawMessage]) Handler[T] {
	return decoded[T]{raw: h}
}

// decoded is the handler Decode returns.
type decoded[T any] struct {
	raw Handler[json.RawMessage]
}

// Read returns the record with its payload decoded into T, or the error of the
// handler underneath unchanged -- ErrExpired above all, which callers branch on.
func (d decoded[T]) Read(ctx context.Context, id string) (Record[T], error) {
	stored, err := d.raw.Read(ctx, id)
	if err != nil {
		return Record[T]{}, err
	}

	var payload T
	// An empty message is a payload nobody wrote, and its zero value is what a
	// typed record would have read back. Unmarshalling nothing is an error in
	// encoding/json, and failing a read over it would sign the session out.
	if len(stored.Payload) > 0 {
		if err := json.Unmarshal(stored.Payload, &payload); err != nil {
			return Record[T]{}, fmt.Errorf("session: decoding the payload of the session record: %w", err)
		}
	}

	return Record[T]{
		Payload:             payload,
		Tenant:              stored.Tenant,
		SubjectID:           stored.SubjectID,
		Remembered:          stored.Remembered,
		PasswordConfirmedAt: stored.PasswordConfirmedAt,
	}, nil
}

// Write encodes the payload and stores the record under id for ttl.
func (d decoded[T]) Write(ctx context.Context, id string, rec Record[T], ttl time.Duration) error {
	payload, err := json.Marshal(rec.Payload)
	if err != nil {
		return fmt.Errorf("session: encoding the payload of the session record: %w", err)
	}

	return d.raw.Write(ctx, id, Record[json.RawMessage]{
		Payload:             payload,
		Tenant:              rec.Tenant,
		SubjectID:           rec.SubjectID,
		Remembered:          rec.Remembered,
		PasswordConfirmedAt: rec.PasswordConfirmedAt,
	}, ttl)
}

// Destroy removes the session, if present.
func (d decoded[T]) Destroy(ctx context.Context, id string) error {
	return d.raw.Destroy(ctx, id)
}

// DestroyIndex removes every session of one subject of one tenant except
// keepID. The refusals of an empty tenant or subject are the handler's own.
func (d decoded[T]) DestroyIndex(ctx context.Context, tenant, subjectID, keepID string) error {
	return d.raw.DestroyIndex(ctx, tenant, subjectID, keepID)
}
