package fakes

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type welcomeMail struct{ To string }

func (m welcomeMail) HasTo(address string) bool  { return m.To == address }
func (m welcomeMail) HasCC(address string) bool  { return false }
func (m welcomeMail) HasBCC(address string) bool { return false }

type receiptMail struct{ Number int }

type newsletterMail struct{}

// ShouldQueue answers that this mailable goes on the queue rather than out now.
func (m newsletterMail) ShouldQueue() bool { return true }

func TestMailFakeAssertSent(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.Send(welcomeMail{To: "a@x"})

	r := &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[welcomeMail](), nil)
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), nil)
	assertFails(t, r, "receiptMail", "not sent", "fakes.welcomeMail")
}

func TestMailFakeAssertSentToAnAddress(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.To("a@x", "b@x").Send(receiptMail{Number: 1})

	r := &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), "a@x")
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), []string{"a@x", "b@x"})
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), "c@x")
	assertFails(t, r, "c@x", "a@x, b@x")

	// A mailable that carries its own recipient answers for it, without the
	// pending mail having collected anything.
	mailer = NewMailFake()
	mailer.Send(welcomeMail{To: "a@x"})

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[welcomeMail](), "a@x")
	assertPasses(t, r)
}

func TestMailFakeQueuedIsNotSent(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	// A mailable that says it belongs on the queue is queued by Send, as the
	// PHP queues it.
	mailer.Send(newsletterMail{})

	r := &recorder{}
	mailer.AssertQueued(r, reflect.TypeFor[newsletterMail](), nil)
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertNothingSent(r)
	assertPasses(t, r)

	// And the failure points at the assertion that would have answered.
	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[newsletterMail](), nil)
	assertFails(t, r, "Did you mean to use AssertQueued()")

	// SendNow overrides it, as it does there.
	mailer = NewMailFake()
	mailer.SendNow(newsletterMail{})

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[newsletterMail](), nil)
	assertPasses(t, r)
}

func TestMailFakeCounts(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.Send(welcomeMail{To: "a@x"})
	mailer.Queue(receiptMail{Number: 1}, "mail")

	r := &recorder{}
	mailer.AssertSentCount(r, 1)
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertQueuedCount(r, 1)
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertOutgoingCount(r, 2)
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertOutgoingCount(r, 3)
	assertFails(t, r, "was 2 instead of 3")

	r = &recorder{}
	mailer.AssertNothingOutgoing(r)
	assertFails(t, r, "sent unexpectedly")
}

func TestMailFakeMailerIsClearedAfterOneMessage(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.Mailer("ses").Send(welcomeMail{To: "a@x"})
	mailer.Send(welcomeMail{To: "b@x"})

	r := &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), nil)
	// The first went through ses and the second did not: the mailer is cleared
	// after each message, as the PHP clears it.
	assertFails(t, r, "through the [ses] mailer")

	mailer = NewMailFake()
	mailer.Mailer("ses").ForgetMailers().Send(welcomeMail{To: "a@x"})
	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[receiptMail](), nil)
	if strings.Contains(r.message(), "through the [ses] mailer") {
		t.Errorf("ForgetMailers should have dropped the mailer, the message said:\n%s", r.message())
	}
}

func TestMailFakePendingMailQueuesAndSends(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.To("a@x").CC("b@x").BCC("c@x").Locale("pt_BR").Queue(newsletterMail{})

	r := &recorder{}
	mailer.AssertQueued(r, reflect.TypeFor[newsletterMail](), "a@x")
	assertPasses(t, r)

	r = &recorder{}
	mailer.AssertNothingSent(r)
	assertPasses(t, r)

	// PendingMailFake::send records as sent whatever the mailable implements.
	mailer = NewMailFake()
	mailer.To("a@x").Send(newsletterMail{})

	r = &recorder{}
	mailer.AssertSent(r, reflect.TypeFor[newsletterMail](), nil)
	assertPasses(t, r)

	// Later does not sleep.
	start := time.Now()
	mailer.To("a@x").Later(time.Hour, receiptMail{})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Later took %s, want no wait at all", elapsed)
	}
	r = &recorder{}
	mailer.AssertQueued(r, reflect.TypeFor[receiptMail](), nil)
	assertPasses(t, r)
}

func TestMailFakeIgnoresANilMailable(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.Send(nil)
	mailer.To("a@x").Send(nil)
	// A raw message has no mailable to record, so there is nothing to assert.
	mailer.Raw("hello", nil)

	r := &recorder{}
	mailer.AssertNothingOutgoing(r)
	assertPasses(t, r)
}

func TestMailFakeSentAndHasSent(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	mailer.Send(welcomeMail{To: "a@x"})

	if !mailer.HasSent(reflect.TypeFor[welcomeMail]()) {
		t.Error("HasSent should answer for a mailable that was sent")
	}
	if mailer.HasQueued(reflect.TypeFor[welcomeMail]()) {
		t.Error("HasQueued should not answer for a mailable that was sent")
	}
	if got := len(mailer.Sent(reflect.TypeFor[welcomeMail](), nil)); got != 1 {
		t.Errorf("Sent = %d, want 1", got)
	}
	if got := len(mailer.Queued(reflect.TypeFor[welcomeMail](), nil)); got != 0 {
		t.Errorf("Queued = %d, want 0", got)
	}
}

func TestMailFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	mailer := NewMailFake()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mailer.To("a@x").Send(welcomeMail{To: "a@x"})
			mailer.HasSent(reflect.TypeFor[welcomeMail]())
		}()
	}
	wg.Wait()

	r := &recorder{}
	mailer.AssertSentCount(r, 50)
	assertPasses(t, r)
}
