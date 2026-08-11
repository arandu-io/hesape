package transport_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/mail/transport"
)

// TestTheArrayTransportIsWhatATestReads. Proving an e-mail was sent needs the
// message, and a transport that returns nil proves nothing.
func TestTheArrayTransportIsWhatATestReads(t *testing.T) {
	box := &transport.Array{}
	m := testMailer(box, echoView{}, "app@example.test")
	ctx := context.Background()

	_, _ = m.To("a@example.test").Send(ctx, welcome{Name: "A", Body: "one"})
	_, _ = m.To("b@example.test").Send(ctx, welcome{Name: "B", Body: "two"})

	if n := len(box.Messages()); n != 2 {
		t.Fatalf("kept %d messages, want 2", n)
	}
	if last, _ := box.Last(); last.Text != "two" {
		t.Errorf("Last is not the most recent: %q", last.Text)
	}

	box.Flush()
	if n := len(box.Messages()); n != 0 {
		t.Errorf("Reset left %d", n)
	}
}

// TestTheArrayTransportIsSafeUnderRace. A test that sends from two goroutines
// and reads from a third would otherwise fail for a reason that has nothing to
// do with what it is proving.
func TestTheArrayTransportIsSafeUnderRace(t *testing.T) {
	box := &transport.Array{}
	m := testMailer(box, echoView{}, "app@example.test")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.To("a@example.test").Send(context.Background(), welcome{Name: "A", Body: "one"})
			_ = box.Messages()
		}()
	}
	wg.Wait()

	if n := len(box.Messages()); n != 8 {
		t.Errorf("kept %d messages, want 8", n)
	}
}

// welcome is the shape a person writes: an envelope, a content, and nothing else.
type welcome struct {
	Name string
	Body string
}

func (w welcome) Envelope() mail.Envelope { return mail.Envelope{Subject: "Welcome, " + w.Name} }
func (w welcome) Content() mail.Content   { return mail.Content{Text: w.Body} }

// testMailer is mail.New with the mailer name and the global sender these tests
// do not care about filled in.
func testMailer(tr mail.Transport, views mail.Renderer, from string) *mail.Mailer {
	m := mail.New("test", views, tr, nil)
	m.AlwaysFrom(from)
	return m
}

// echoView is a Renderer that answers with the name of the view it was asked
// for, so a test can put the body it wants straight into Content.Text --
// which, as in Illuminate, is a view name and not a body.
type echoView struct{}

func (echoView) RenderToString(name string, _ any) (string, error) { return name, nil }

// TestFailoverStopsAtTheFirstTransportThatAccepts, and does not send twice: the
// second delivery of a password reset is a second link, and the first one stops
// working.
func TestFailoverStopsAtTheFirstTransportThatAccepts(t *testing.T) {
	broken := &counting{err: errors.New("down")}
	good := &counting{id: "ok-1"}
	spare := &counting{id: "never"}

	f := transport.Failover{Transports: []mail.Transport{broken, good, spare}}
	sent, err := testMailer(f, echoView{}, "app@example.test").
		To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("nothing accepted the message: %v", err)
	}

	if sent.ID != "ok-1" {
		t.Errorf("ID is %q, want the second transport's", sent.ID)
	}
	if sent.Transport != good.Name() {
		t.Errorf("Transport is %q: the caller cannot tell which one delivered", sent.Transport)
	}
	if spare.calls != 0 {
		t.Errorf("the third transport sent the message again: %d calls", spare.calls)
	}
}

// TestFailoverNamesEveryTransportThatRefused. "Sending failed" with three
// providers configured sends somebody to three dashboards.
func TestFailoverNamesEveryTransportThatRefused(t *testing.T) {
	f := transport.Failover{Transports: []mail.Transport{
		&counting{name: "first", err: errors.New("the domain is not verified")},
		&counting{name: "second", err: errors.New("bad key")},
	}}

	_, err := testMailer(f, echoView{}, "app@example.test").
		To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err == nil {
		t.Fatal("a total failure was reported as a send")
	}
	for _, want := range []string{"first", "the domain is not verified", "second", "bad key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// TestFailoverIsRetryableWhenAnyTransportMightAcceptLater.
//
// A provider that answered 503 may take the same message in a minute, so the
// job reschedules -- even though another provider rejected it outright. Only
// when every refusal was permanent is retrying pointless, and that is the case
// where the message itself is the problem.
func TestFailoverIsRetryableWhenAnyTransportMightAcceptLater(t *testing.T) {
	for _, c := range []struct {
		name    string
		first   error
		second  error
		retries bool
	}{
		{"both down", mail.Retryable(errors.New("503")), mail.Retryable(errors.New("502")), true},
		{"one down, one rejected", mail.Retryable(errors.New("503")), errors.New("422: not a mailbox"), true},
		{"both rejected", errors.New("422: not a mailbox"), errors.New("422: unknown domain"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := transport.Failover{Transports: []mail.Transport{
				&counting{name: "first", err: c.first},
				&counting{name: "second", err: c.second},
			}}

			_, err := testMailer(f, echoView{}, "app@example.test").
				To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
			if err == nil {
				t.Fatal("a total failure was reported as a send")
			}

			var retryable mail.ErrRetryable
			if got := errors.As(err, &retryable); got != c.retries {
				t.Errorf("retryable is %v, want %v: %v", got, c.retries, err)
			}
		})
	}
}

// TestFailoverWithNothingToFailOverToIsAnError, rather than a message that goes
// nowhere and reports success.
func TestFailoverWithNothingToFailOverToIsAnError(t *testing.T) {
	_, err := testMailer(transport.Failover{}, echoView{}, "app@example.test").
		To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err == nil {
		t.Fatal("an empty failover reported a send")
	}
}

// TestFailoverStopsWhenTheRequestIsGone. Trying the next provider only spends
// time the caller stopped waiting for.
func TestFailoverStopsWhenTheRequestIsGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	second := &counting{name: "second", err: errors.New("down")}
	f := transport.Failover{Transports: []mail.Transport{
		&counting{name: "first", err: errors.New("down")},
		second,
	}}

	if _, err := testMailer(f, echoView{}, "app@example.test").
		To("you@example.test").Send(ctx, welcome{Name: "Ada", Body: "hi"}); err == nil {
		t.Fatal("a cancelled send reported success")
	}
	if second.calls != 0 {
		t.Errorf("it kept trying after the request was cancelled: %d calls", second.calls)
	}
}

// counting is a transport that answers as told and remembers how often it was
// asked.
type counting struct {
	name  string
	id    string
	err   error
	calls int
}

func (c *counting) Name() string {
	if c.name == "" {
		return "counting"
	}
	return c.name
}

func (c *counting) Send(context.Context, mail.Message) (mail.SentMessage, error) {
	c.calls++
	if c.err != nil {
		return mail.SentMessage{}, c.err
	}
	return mail.SentMessage{ID: c.id, Transport: c.Name()}, nil
}
