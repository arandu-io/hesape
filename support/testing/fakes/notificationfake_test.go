package fakes

import (
	"reflect"
	"sync"
	"testing"
)

type user struct {
	Key    string
	Locale string
}

func (u user) GetKey() string { return u.Key }

type localeUser struct {
	Key string
}

func (u localeUser) GetKey() string          { return u.Key }
func (u localeUser) PreferredLocale() string { return "pt_BR" }

type invoicePaid struct {
	Amount   int
	Channels []string

	id     string
	locale string
}

func (n *invoicePaid) Via(notifiable any) []string {
	if len(n.Channels) > 0 {
		return n.Channels
	}
	return []string{"mail"}
}

func (n *invoicePaid) ID() string                 { return n.id }
func (n *invoicePaid) SetID(id string)            { n.id = id }
func (n *invoicePaid) NotificationLocale() string { return n.locale }

type welcomeNote struct{}

func (n welcomeNote) Via(notifiable any) []string { return []string{"mail", "database"} }

type quietNote struct{}

func (n quietNote) Via(notifiable any) []string { return []string{"mail", "sms"} }

func (n quietNote) ShouldSend(notifiable any, channel string) bool { return channel == "mail" }

type silentNote struct{}

func (n silentNote) Via(notifiable any) []string { return nil }

func TestNotificationFakeAssertSentTo(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, &invoicePaid{Amount: 10})

	r := &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[invoicePaid](), nil)
	assertPasses(t, r)

	// The key is part of the address: the same class with another key is
	// another person.
	r = &recorder{}
	notifications.AssertSentTo(r, user{Key: "2"}, reflect.TypeFor[invoicePaid](), nil)
	assertFails(t, r, "fakes.user#2", "1 notification was sent", "fakes.user#1")

	r = &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), nil)
	assertFails(t, r, "fakes.welcomeNote", "fakes.invoicePaid")
}

func TestNotificationFakeTruthTestsSeeChannelsAndNotifiable(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, welcomeNote{})

	r := &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), func(n any, channels []string) bool {
		return len(channels) == 2 && channels[0] == "mail"
	})
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), func(n any, channels []string, notifiable any) bool {
		return notifiable.(user).Key == "1"
	})
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), "not a callback")
	assertFails(t, r, "the callback must be nil", "got string")
}

func TestNotificationFakeAssertSentToASliceOfNotifiables(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send([]any{user{Key: "1"}, user{Key: "2"}}, welcomeNote{})

	r := &recorder{}
	notifications.AssertSentTo(r, []any{user{Key: "1"}, user{Key: "2"}}, reflect.TypeFor[welcomeNote](), nil)
	assertPasses(t, r)

	// An assertion over nothing passes without checking anything, so an empty
	// slice is the caller's mistake, as it is in the PHP.
	r = &recorder{}
	notifications.AssertSentTo(r, []any{}, reflect.TypeFor[welcomeNote](), nil)
	assertFails(t, r, "no notifiable given")

	r = &recorder{}
	notifications.AssertNotSentTo(r, []any{}, reflect.TypeFor[welcomeNote](), nil)
	assertFails(t, r, "no notifiable given")

	r = &recorder{}
	notifications.AssertNothingSentTo(r, []any{})
	assertFails(t, r, "no notifiable given")
}

func TestNotificationFakeAssertSentToTimes(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, welcomeNote{})
	notifications.Send(user{Key: "1"}, welcomeNote{})
	notifications.Send(user{Key: "2"}, welcomeNote{})

	r := &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), 2)
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertSentToTimes(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), 3)
	assertFails(t, r, "sent to fakes.user#1 3 times, but it was sent 2 times")

	// AssertSentTimes counts everybody, not one notifiable.
	r = &recorder{}
	notifications.AssertSentTimes(r, reflect.TypeFor[welcomeNote](), 3)
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertSentTimes(r, reflect.TypeFor[welcomeNote](), 2)
	assertFails(t, r, "sent 2 times, but it was sent 3 times")
}

func TestNotificationFakeAssertNothingSent(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()

	r := &recorder{}
	notifications.AssertNothingSent(r)
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertNothingSentTo(r, user{Key: "1"})
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertCount(r, 0)
	assertPasses(t, r)

	notifications.Send(user{Key: "1"}, welcomeNote{})

	r = &recorder{}
	notifications.AssertNothingSent(r)
	assertFails(t, r, "sent unexpectedly", "fakes.welcomeNote", "fakes.user#1", "mail, database")

	r = &recorder{}
	notifications.AssertNothingSentTo(r, user{Key: "1"})
	assertFails(t, r, "1 notification was sent to fakes.user#1")

	// Nothing reached the other user, so that assertion still passes.
	r = &recorder{}
	notifications.AssertNothingSentTo(r, user{Key: "2"})
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertCount(r, 2)
	assertFails(t, r, "expected 2 notifications to be sent, but 1 notification was sent")
}

func TestNotificationFakeAssertNotSentTo(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, welcomeNote{})

	r := &recorder{}
	notifications.AssertNotSentTo(r, user{Key: "2"}, reflect.TypeFor[welcomeNote](), nil)
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertNotSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), nil)
	assertFails(t, r, "unexpected [fakes.welcomeNote] notification was sent to fakes.user#1 1 time")
}

func TestNotificationFakeOnDemand(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()

	r := &recorder{}
	notifications.AssertSentOnDemand(r, reflect.TypeFor[welcomeNote](), nil)
	assertFails(t, r, "AnonymousNotifiable")

	notifications.Send(NewAnonymousNotifiable(), welcomeNote{})

	r = &recorder{}
	notifications.AssertSentOnDemand(r, reflect.TypeFor[welcomeNote](), nil)
	assertPasses(t, r)

	r = &recorder{}
	notifications.AssertSentOnDemandTimes(r, reflect.TypeFor[welcomeNote](), 1)
	assertPasses(t, r)
}

func TestAnonymousNotifiableRefusesTheDatabaseChannel(t *testing.T) {
	t.Parallel()

	anonymous := NewAnonymousNotifiable()
	if _, err := anonymous.Route("mail", "a@x"); err != nil {
		t.Fatalf("Route(mail) answered %v, want no error", err)
	}
	if anonymous.Routes["mail"] != "a@x" {
		t.Errorf("Routes[mail] = %q, want a@x", anonymous.Routes["mail"])
	}
	if _, err := anonymous.Route("database", "1"); err == nil {
		t.Error("Route(database) should answer an error, as the PHP throws one")
	}
	if anonymous.GetKey() != "" {
		t.Errorf("GetKey = %q, want empty", anonymous.GetKey())
	}
}

func TestNotificationFakeSkipsANotificationWithNoChannel(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, silentNote{})

	// A notification whose Via comes back empty never lands anywhere, so it
	// leaves no record to assert on.
	r := &recorder{}
	notifications.AssertNothingSent(r)
	assertPasses(t, r)
}

func TestNotificationFakeHonoursShouldSend(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, quietNote{})

	r := &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[quietNote](), func(n any, channels []string) bool {
		return len(channels) == 1 && channels[0] == "mail"
	})
	assertPasses(t, r)
}

func TestNotificationFakeChannelsGivenWinOverVia(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.SendNow(user{Key: "1"}, welcomeNote{}, []string{"slack"})

	r := &recorder{}
	notifications.AssertSentTo(r, user{Key: "1"}, reflect.TypeFor[welcomeNote](), func(n any, channels []string) bool {
		return len(channels) == 1 && channels[0] == "slack"
	})
	assertPasses(t, r)
}

func TestNotificationFakeLocaleFallsThroughThreeWays(t *testing.T) {
	t.Parallel()

	// The notification's own locale wins.
	notifications := NewNotificationFake().Locale("en")
	notifications.Send(user{Key: "1"}, &invoicePaid{locale: "fr"})
	if got := notifications.snapshot()[0].locale; got != "fr" {
		t.Errorf("the record kept the %q locale, want fr", got)
	}

	// Then the fake's.
	notifications = NewNotificationFake().Locale("en")
	notifications.Send(user{Key: "1"}, &invoicePaid{})
	if got := notifications.snapshot()[0].locale; got != "en" {
		t.Errorf("the record kept the %q locale, want en", got)
	}

	// Then what the notifiable would rather read.
	notifications = NewNotificationFake()
	notifications.Send(localeUser{Key: "1"}, &invoicePaid{})
	if got := notifications.snapshot()[0].locale; got != "pt_BR" {
		t.Errorf("the record kept the %q locale, want pt_BR", got)
	}
}

func TestNotificationFakeStampsAnIDOnANotificationWithout(t *testing.T) {
	t.Parallel()

	notification := &invoicePaid{}
	NewNotificationFake().Send(user{Key: "1"}, notification)

	if len(notification.ID()) != 36 {
		t.Errorf("the notification carries the id %q, want a uuid", notification.ID())
	}

	// One it already has is left alone: the same notification sent to ten
	// people is one notification.
	kept := &invoicePaid{id: "already set"}
	NewNotificationFake().Send([]any{user{Key: "1"}, user{Key: "2"}}, kept)
	if kept.ID() != "already set" {
		t.Errorf("the notification's id became %q, want it left alone", kept.ID())
	}
}

func TestNotificationFakeSentAndHasSent(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	notifications.Send(user{Key: "1"}, &invoicePaid{Amount: 10})

	if !notifications.HasSent(user{Key: "1"}, reflect.TypeFor[invoicePaid]()) {
		t.Error("HasSent should answer for a notification that was sent")
	}
	if notifications.HasSent(user{Key: "2"}, reflect.TypeFor[invoicePaid]()) {
		t.Error("HasSent should not answer for a notifiable that got nothing")
	}

	sent := notifications.Sent(user{Key: "1"}, reflect.TypeFor[invoicePaid](), nil)
	if len(sent) != 1 || sent[0].(*invoicePaid).Amount != 10 {
		t.Errorf("Sent = %v, want the one notification", sent)
	}

	byClass := notifications.SentNotifications()
	if got := len(byClass["fakes.user"]["1"]); got != 1 {
		t.Errorf("SentNotifications[fakes.user][1] = %d, want 1", got)
	}

	if notifications.Channel("mail") != nil {
		t.Error("Channel should answer nothing, as the PHP does")
	}
}

func TestNotificationFakeSerializeAndRestore(t *testing.T) {
	t.Parallel()

	// Only a queued notification takes the round trip, as in the PHP.
	notifications := NewNotificationFake().SerializeAndRestore(true)
	inline := &invoicePaid{Amount: 10}
	notifications.Send(user{Key: "1"}, inline)

	sent := notifications.Sent(user{Key: "1"}, reflect.TypeFor[invoicePaid](), nil)
	if len(sent) != 1 || sent[0] != any(inline) {
		t.Error("a notification that is not queued should be recorded as it stands")
	}
}

func TestNotificationFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	notifications := NewNotificationFake()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			notifications.Send(user{Key: "1"}, welcomeNote{})
			notifications.HasSent(user{Key: "1"}, reflect.TypeFor[welcomeNote]())
		}()
	}
	wg.Wait()

	r := &recorder{}
	notifications.AssertCount(r, 50)
	assertPasses(t, r)
}
