package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
)

// subject is a payload shaped like the one an application keeps: a struct with
// tags, a slice, and text that encoding/json escapes.
type subject struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
	Note  string   `json:"note,omitempty"`
}

const (
	rawTenant  = "11111111-1111-4111-8111-111111111111"
	rawSubject = "u-42"
)

func typedRecord() session.Record[subject] {
	return session.Record[subject]{
		Payload: subject{
			ID:    rawSubject,
			Roles: []string{"admin", "billing"},
			// <, > and & are escaped by encoding/json, and a non-ASCII rune is
			// not: the two places a re-encoding most often changes bytes.
			Note: "<b>R&D</b> — São Paulo",
		},
		Tenant:              rawTenant,
		SubjectID:           rawSubject,
		Remembered:          true,
		PasswordConfirmedAt: time.Date(2026, 9, 30, 14, 5, 6, 123456789, time.FixedZone("BRT", -3*60*60)),
	}
}

// TestDecodeRoundTripsATypedPayload: what goes in through the typed handler
// comes back out of it, payload and index alike.
func TestDecodeRoundTripsATypedPayload(t *testing.T) {
	ctx := context.Background()
	h := session.Decode[subject](session.NewArrayHandler[json.RawMessage]())
	want := typedRecord()

	if err := h.Write(ctx, "s-1", want, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := h.Read(ctx, "s-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// TestDecodePassesTheIndexThroughUntouched: Tenant and SubjectID are what a
// handler signs a subject out by. The raw handler underneath has to see them as
// fields, not buried in the payload, or "sign this account out everywhere"
// finds nothing.
func TestDecodePassesTheIndexThroughUntouched(t *testing.T) {
	ctx := context.Background()
	raw := session.NewArrayHandler[json.RawMessage]()
	h := session.Decode[subject](raw)

	if err := h.Write(ctx, "s-1", typedRecord(), time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	stored, err := raw.Read(ctx, "s-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if stored.Tenant != rawTenant || stored.SubjectID != rawSubject {
		t.Errorf("the raw handler holds tenant %q and subject %q, want %q and %q",
			stored.Tenant, stored.SubjectID, rawTenant, rawSubject)
	}
	if !stored.Remembered || !stored.PasswordConfirmedAt.Equal(typedRecord().PasswordConfirmedAt) {
		t.Errorf("the lifetime fields did not reach the raw handler: %+v", stored)
	}

	encoded, _ := json.Marshal(typedRecord().Payload)
	if !bytes.Equal(stored.Payload, encoded) {
		t.Errorf("the raw payload is %s, want the payload's own JSON %s", stored.Payload, encoded)
	}

	// And the bulk sign-out reaches it by those fields.
	if err := h.DestroyIndex(ctx, rawTenant, rawSubject, ""); err != nil {
		t.Fatalf("DestroyIndex: %v", err)
	}
	if _, err := h.Read(ctx, "s-1"); !errors.Is(err, session.ErrExpired) {
		t.Errorf("after DestroyIndex, Read = %v, want ErrExpired", err)
	}
}

// TestARawRecordSerializesAsTheTypedOne is the compatibility the whole shape
// rests on: a handler that marshals the record writes the same bytes for a
// Record[T] and for the Record[json.RawMessage] Decode builds from it, so a
// session written before the switch reads back after it, and the reverse.
func TestARawRecordSerializesAsTheTypedOne(t *testing.T) {
	typed := typedRecord()
	typedBytes, err := json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(typed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := json.Marshal(session.Record[json.RawMessage]{
		Payload:             payload,
		Tenant:              typed.Tenant,
		SubjectID:           typed.SubjectID,
		Remembered:          typed.Remembered,
		PasswordConfirmedAt: typed.PasswordConfirmedAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(typedBytes, rawBytes) {
		t.Fatalf("the two encodings differ:\ntyped %s\nraw   %s", typedBytes, rawBytes)
	}

	// Old bytes into the new shape, and back out typed.
	var asRaw session.Record[json.RawMessage]
	if err := json.Unmarshal(typedBytes, &asRaw); err != nil {
		t.Fatalf("a typed record does not decode as a raw one: %v", err)
	}
	var payloadBack subject
	if err := json.Unmarshal(asRaw.Payload, &payloadBack); err != nil {
		t.Fatalf("the raw payload does not decode as the type it came from: %v", err)
	}
	if !reflect.DeepEqual(payloadBack, typed.Payload) || asRaw.Tenant != typed.Tenant || asRaw.SubjectID != typed.SubjectID {
		t.Errorf("a typed record read through the raw shape lost something: %+v", asRaw)
	}
}

// TestDecodeLeavesExpiredAsItIs: callers branch on ErrExpired to send somebody
// to the login page, and a wrapper that rewrapped it would turn an expired
// session into an error page.
func TestDecodeLeavesExpiredAsItIs(t *testing.T) {
	h := session.Decode[subject](session.NewArrayHandler[json.RawMessage]())

	if _, err := h.Read(context.Background(), "unknown"); err != session.ErrExpired {
		t.Errorf("Read = %v, want ErrExpired itself", err)
	}
}

// TestDecodeKeepsTheHandlersRefusals: a bulk sign-out with no tenant reaches
// every customer, and the handler underneath is what refuses it.
func TestDecodeKeepsTheHandlersRefusals(t *testing.T) {
	h := session.Decode[subject](session.NewArrayHandler[json.RawMessage]())

	if err := h.DestroyIndex(context.Background(), "", rawSubject, ""); err == nil {
		t.Error("DestroyIndex with no tenant was accepted")
	}
}

// TestAPayloadOfAnotherShapeIsAnErrorNotAnExpiry: a record the type cannot
// hold is a defect to report, and reading it as expired would hide it behind a
// login page.
func TestAPayloadOfAnotherShapeIsAnErrorNotAnExpiry(t *testing.T) {
	ctx := context.Background()
	raw := session.NewArrayHandler[json.RawMessage]()
	if err := raw.Write(ctx, "s-1", session.Record[json.RawMessage]{Payload: json.RawMessage(`"a string"`)}, time.Hour); err != nil {
		t.Fatal(err)
	}

	_, err := session.Decode[subject](raw).Read(ctx, "s-1")
	if err == nil || errors.Is(err, session.ErrExpired) {
		t.Errorf("Read = %v, want a decoding error", err)
	}
}

// TestAnEmptyPayloadReadsAsTheZeroValue: a record written with no payload --
// a guest's -- is a session, not a decoding failure.
func TestAnEmptyPayloadReadsAsTheZeroValue(t *testing.T) {
	ctx := context.Background()
	raw := session.NewArrayHandler[json.RawMessage]()
	if err := raw.Write(ctx, "s-1", session.Record[json.RawMessage]{Tenant: rawTenant}, time.Hour); err != nil {
		t.Fatal(err)
	}

	got, err := session.Decode[subject](raw).Read(ctx, "s-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got.Payload, subject{}) || got.Tenant != rawTenant {
		t.Errorf("read back %+v, want the zero payload and the tenant", got)
	}
}

// TestDecodeCarriesEveryFieldOfTheRecord: Decode copies the record field by
// field, so a field added to Record is a field Decode drops until it is copied
// too -- and a dropped field reads back as its zero value, which is the defect
// Handler.Write warns about. This fails the moment the count moves.
func TestDecodeCarriesEveryFieldOfTheRecord(t *testing.T) {
	if n := reflect.TypeFor[session.Record[subject]]().NumField(); n != 5 {
		t.Errorf("Record has %d fields and Decode copies 5: carry the new one through Read and Write", n)
	}
}
