package notifications_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/notifications"
)

func TestCaptureRecordsWhatWouldHaveBeenSent(t *testing.T) {
	chans, sent := notifications.Capture(notifications.ChannelMail, notifications.ChannelDatabase)
	n := notifications.New(chans)
	ada := user{id: "u1", email: "ada@example.com"}

	if err := n.Send(context.Background(), sendGrant(t), ada, invoicePaid{number: "2026-114"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.Len() != 2 {
		t.Fatalf("recorded %d deliveries, want 2", sent.Len())
	}
	if !sent.Sent("billing.invoice-paid", ada) {
		t.Fatal("Sent says the invoice notification never reached her")
	}
	if sent.Sent("billing.invoice-paid", user{id: "u2"}) {
		t.Fatal("Sent says it reached somebody it was never addressed to")
	}

	byMail := sent.For("billing.invoice-paid")
	if len(byMail) != 2 {
		t.Fatalf("For returned %d", len(byMail))
	}
	first := byMail[0]
	if first.Channel != notifications.ChannelMail || first.Route != "ada@example.com" {
		t.Fatalf("first delivery = %+v", first)
	}
	if first.Tenant != "acme" {
		t.Fatalf("tenant = %q", first.Tenant)
	}
	note, ok := first.Notification.(invoicePaid)
	if !ok || note.number != "2026-114" {
		t.Fatalf("the notification itself was not recorded: %+v", first.Notification)
	}

	sent.Reset()
	if sent.Len() != 0 {
		t.Fatal("Reset kept the deliveries")
	}
}

func TestCaptureWithNoNamesTakesTheThreeBuiltIn(t *testing.T) {
	chans, _ := notifications.Capture()
	if len(chans) != 3 {
		t.Fatalf("captured %d channels, want the three the collection implements", len(chans))
	}
	n := notifications.New(chans)
	want := []notifications.ChannelName{notifications.ChannelBroadcast, notifications.ChannelDatabase, notifications.ChannelMail}
	got := n.Channels()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("channels = %v, want %v", got, want)
		}
	}
}
