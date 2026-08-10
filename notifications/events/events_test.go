package events_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/notifications/events"
)

func payload() events.Payload {
	return events.Payload{
		Key:            "billing.invoice-paid",
		Channel:        "mail",
		NotifiableType: "user",
		NotifiableID:   "u1",
		Tenant:         "acme",
	}
}

func TestTheThreeEventsNameWhatHappened(t *testing.T) {
	sending := events.NewSending(payload())
	if sending.Name != events.Sending {
		t.Fatalf("name = %q", sending.Name)
	}
	if sending.Aggregate != events.Aggregate || sending.AggregateID != "user:u1" {
		t.Fatalf("aggregate = %s/%s", sending.Aggregate, sending.AggregateID)
	}
	if sending.OccurredAt.IsZero() {
		t.Fatal("the event has no time on it")
	}

	sent := events.NewSent(payload(), "provider-7")
	if sent.Name != events.Sent {
		t.Fatalf("name = %q", sent.Name)
	}
	if sent.Payload.(events.Payload).Receipt != "provider-7" {
		t.Fatalf("payload = %+v", sent.Payload)
	}

	failed := events.NewFailed(payload(), errors.New("the provider is down"))
	if failed.Name != events.Failed {
		t.Fatalf("name = %q", failed.Name)
	}
	if got := failed.Payload.(events.Payload).Error; got != "the provider is down" {
		t.Fatalf("error = %q", got)
	}
}

func TestThePayloadEncodesWithoutTheMessageBody(t *testing.T) {
	raw, err := json.Marshal(events.NewSent(payload(), "provider-7").Payload)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	// An outbox row is read by everything downstream, so it carries names and
	// ids and never the body -- which is somebody's reset link.
	for _, want := range []string{`"key":"billing.invoice-paid"`, `"channel":"mail"`, `"tenant":"acme"`, `"receipt":"provider-7"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("payload is missing %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"error"`) {
		t.Fatalf("a successful send carries an error field: %s", raw)
	}
}
