package transport_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/mail/transport"
)

// provider is a stand-in for resend.com, sendgrid.com or postmarkapp.com: it
// records the request and answers with what the test asked it to.
type provider struct {
	body    map[string]any
	headers http.Header
	status  int
	answer  string
	// answerHeaders is what the provider sets on the way back, which is where
	// SendGrid puts the message identifier.
	answerHeaders map[string]string
}

func (p *provider) serve(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &p.body)

		for k, v := range p.answerHeaders {
			w.Header().Set(k, v)
		}
		if p.status == 0 {
			p.status = http.StatusOK
		}
		w.WriteHeader(p.status)
		_, _ = io.WriteString(w, p.answer)
	}))
	t.Cleanup(s.Close)
	return s
}

func (p *provider) auth() string { return p.headers.Get("Authorization") }

// note is a mailable with both parts, which is the shape every message the
// framework itself sends has.
type note struct{ to string }

func (note) Envelope() mail.Envelope {
	return mail.Envelope{
		From:    mail.Address{Address: "blog@example.com", Name: "The Blog"},
		Subject: "Confirm your address",
		Tags:    []string{"account verification"},
	}
}

func (note) Content() mail.Content {
	return mail.Content{Text: "Open the link."}
}

func send(t *testing.T, tr mail.Transport, to string) (mail.SentMessage, error) {
	t.Helper()
	return testMailer(tr, echoView{}, "blog@example.com").
		To(to).Send(context.Background(), note{to: to})
}

// TestResendSendsWhatTheEnvelopeSaid.
//
// The body is the whole transport. A field renamed by the provider is a message
// that is accepted and delivered to nobody, or delivered without its subject.
func TestResendSendsWhatTheEnvelopeSaid(t *testing.T) {
	p := &provider{answer: `{"id":"abc"}`}
	server := p.serve(t)

	sent, err := send(t, transport.Resend{Key: "re_test", Endpoint: server.URL}, "reader@example.com")
	if err != nil {
		t.Fatalf("the message was not accepted: %v", err)
	}

	if p.auth() != "Bearer re_test" {
		t.Errorf("the key did not reach the provider: %q", p.auth())
	}
	if got := p.body["from"]; got != `"The Blog" <blog@example.com>` {
		t.Errorf("from is %v", got)
	}
	if got := p.body["subject"]; got != "Confirm your address" {
		t.Errorf("subject is %v", got)
	}
	to, _ := p.body["to"].([]any)
	if len(to) != 1 || to[0] != "reader@example.com" {
		t.Errorf("to is %v", p.body["to"])
	}
	if _, ok := p.body["html"]; ok {
		t.Error("a message with no HTML part sent an empty one, which renders as a blank e-mail")
	}
	if sent.ID != "abc" {
		t.Errorf("ID is %q: the provider's identifier is the only way back to its dashboard", sent.ID)
	}
	if sent.Transport != "resend" {
		t.Errorf("Transport is %q", sent.Transport)
	}
}

// TestResendTagsAreReducedToWhatTheProviderAccepts.
//
// Resend rejects a tag containing anything outside [A-Za-z0-9_-], and it rejects
// the whole message rather than the tag. A space in a tag would drop the
// e-mail -- a verification link lost to a label.
func TestResendTagsAreReducedToWhatTheProviderAccepts(t *testing.T) {
	p := &provider{answer: `{"id":"abc"}`}
	server := p.serve(t)

	if _, err := send(t, transport.Resend{Key: "k", Endpoint: server.URL}, "reader@example.com"); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(p.body["tags"])
	if strings.Contains(string(raw), " ") {
		t.Errorf("a tag went out with a space in it: %s", raw)
	}
	if !strings.Contains(string(raw), "account_verification") {
		t.Errorf("the tag did not survive: %s", raw)
	}
}

// TestARateLimitIsRetryableAndARejectedAddressIsNot.
//
// They are different events and the caller acts differently on them. A 429
// treated as final is a verification e-mail silently lost; a bad address treated
// as retryable is a job that fails forever.
func TestARateLimitIsRetryableAndARejectedAddressIsNot(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		retries bool
	}{
		{"rate limited", http.StatusTooManyRequests, true},
		{"provider down", http.StatusBadGateway, true},
		{"rejected", http.StatusUnprocessableEntity, false},
		{"bad key", http.StatusUnauthorized, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &provider{status: c.status, answer: `{"name":"x","message":"nope"}`}
			server := p.serve(t)

			_, err := send(t, transport.Resend{Key: "k", Endpoint: server.URL}, "reader@example.com")
			if err == nil {
				t.Fatal("a failure was reported as a send")
			}

			var retryable mail.ErrRetryable
			if got := errors.As(err, &retryable); got != c.retries {
				t.Errorf("retryable is %v, want %v: %v", got, c.retries, err)
			}
		})
	}
}

// TestTheProvidersMessageReachesTheCaller: "the provider answered 422" alone
// sends somebody to a dashboard to find out what was wrong with the address.
func TestTheProvidersMessageReachesTheCaller(t *testing.T) {
	p := &provider{status: 422, answer: `{"name":"validation_error","message":"The domain is not verified"}`}
	server := p.serve(t)

	_, err := send(t, transport.Resend{Key: "k", Endpoint: server.URL}, "reader@example.com")
	if err == nil || !strings.Contains(err.Error(), "domain is not verified") {
		t.Fatalf("the provider's reason did not reach the caller: %v", err)
	}
}

// TestSendGridPutsTextBeforeHTML.
//
// SendGrid sends the parts in the order they arrive and MIME says the richest
// alternative comes last. Reversed, every client shows the plain-text version of
// a message that has HTML -- which looks like a broken template, not a bug here.
func TestSendGridPutsTextBeforeHTML(t *testing.T) {
	p := &provider{status: http.StatusAccepted}
	server := p.serve(t)

	_, err := testMailer(transport.SendGrid{Key: "SG.k", Endpoint: server.URL}, fixedView{}, "blog@example.com").
		To("reader@example.com").Send(context.Background(), bothParts{})
	if err != nil {
		t.Fatalf("the message was not accepted: %v", err)
	}

	content, _ := p.body["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("both parts did not arrive: %v", p.body["content"])
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text/plain" {
		t.Errorf("the first part is %v: the client will show it instead of the HTML", first["type"])
	}
}

// TestSendGridSendsOneMessageAndNotOnePerRecipient.
//
// One personalization per recipient is a different SendGrid feature -- a
// separate message each -- and using it here turns one message to three people
// into three messages, each of which looks fine on its own.
func TestSendGridSendsOneMessageAndNotOnePerRecipient(t *testing.T) {
	p := &provider{status: http.StatusAccepted}
	server := p.serve(t)

	_, err := testMailer(transport.SendGrid{Key: "SG.k", Endpoint: server.URL}, fixedView{}, "blog@example.com").
		To([]string{"a@example.com", "b@example.com"}).CC("c@example.com").
		Send(context.Background(), bothParts{})
	if err != nil {
		t.Fatal(err)
	}

	people, _ := p.body["personalizations"].([]any)
	if len(people) != 1 {
		t.Fatalf("%d personalizations: that is %d separate messages", len(people), len(people))
	}
	first, _ := people[0].(map[string]any)
	if to, _ := first["to"].([]any); len(to) != 2 {
		t.Errorf("%d recipients in the personalization, want 2", len(to))
	}
	if _, ok := first["cc"]; !ok {
		t.Error("the cc was dropped")
	}
}

// TestSendGridReadsItsIdentifierFromTheHeader. SendGrid answers 202 with an
// empty body, so the one place the identifier can come from is a header.
func TestSendGridReadsItsIdentifierFromTheHeader(t *testing.T) {
	p := &provider{
		status:        http.StatusAccepted,
		answerHeaders: map[string]string{"X-Message-Id": "sg-99"},
	}
	server := p.serve(t)

	sent, err := testMailer(transport.SendGrid{Key: "SG.k", Endpoint: server.URL}, fixedView{}, "blog@example.com").
		To("reader@example.com").Send(context.Background(), bothParts{})
	if err != nil {
		t.Fatal(err)
	}
	if sent.ID != "sg-99" {
		t.Errorf("ID is %q, want the one from the header", sent.ID)
	}
}

// TestPostmarkSendsWhatTheEnvelopeSaid. Postmark's field names are its own --
// capitalised, and recipients as one comma-separated string rather than a list.
func TestPostmarkSendsWhatTheEnvelopeSaid(t *testing.T) {
	p := &provider{answer: `{"MessageID":"pm-7","ErrorCode":0,"Message":"OK"}`}
	server := p.serve(t)

	sent, err := testMailer(transport.Postmark{Token: "tok", Stream: "outbound", Endpoint: server.URL}, echoView{}, "blog@example.com").
		To([]string{"a@example.com", "b@example.com"}).Send(context.Background(), note{})
	if err != nil {
		t.Fatalf("the message was not accepted: %v", err)
	}

	if got := p.headers.Get("X-Postmark-Server-Token"); got != "tok" {
		t.Errorf("the token did not reach the provider: %q", got)
	}
	if got := p.body["To"]; got != "a@example.com, b@example.com" {
		t.Errorf("To is %v: Postmark reads one string, not a list", got)
	}
	if got := p.body["Subject"]; got != "Confirm your address" {
		t.Errorf("Subject is %v", got)
	}
	if got := p.body["Tag"]; got != "account verification" {
		t.Errorf("Tag is %v", got)
	}
	if got := p.body["MessageStream"]; got != "outbound" {
		t.Errorf("MessageStream is %v", got)
	}
	if _, ok := p.body["HtmlBody"]; ok {
		t.Error("a message with no HTML part sent an empty one, which renders as a blank e-mail")
	}
	if sent.ID != "pm-7" {
		t.Errorf("ID is %q", sent.ID)
	}
}

// TestPostmarksErrorCodeReachesTheCaller. Postmark documents each failure by
// number, and the number is what somebody searches for.
func TestPostmarksErrorCodeReachesTheCaller(t *testing.T) {
	p := &provider{
		status: http.StatusUnprocessableEntity,
		answer: `{"ErrorCode":300,"Message":"Invalid 'To' address"}`,
	}
	server := p.serve(t)

	_, err := send(t, transport.Postmark{Token: "tok", Endpoint: server.URL}, "reader@example.com")
	if err == nil {
		t.Fatal("a rejection was reported as a send")
	}
	if !strings.Contains(err.Error(), "300") || !strings.Contains(err.Error(), "Invalid 'To' address") {
		t.Errorf("the provider's reason did not reach the caller: %v", err)
	}

	var retryable mail.ErrRetryable
	if errors.As(err, &retryable) {
		t.Error("a rejected address was marked retryable: the job will try it forever")
	}
}

// bothParts is a mailable with an HTML part and a text part.
type bothParts struct{}

func (bothParts) Envelope() mail.Envelope {
	return mail.Envelope{From: mail.Address{Address: "blog@example.com"}, Subject: "Both"}
}
func (bothParts) Content() mail.Content {
	return mail.Content{View: "mail.note", Text: "the text part"}
}

// fixedView is a Renderer that answers with one string for the HTML view and
// with the view's own name for everything else, so that a mailable naming two
// views does not get the same body twice.
type fixedView struct{}

func (fixedView) RenderToString(name string, _ any) (string, error) {
	if name == "mail.note" {
		return "<p>the html part</p>", nil
	}
	return name, nil
}

// TestMeengSendsWhatTheEnvelopeSaid.
//
// The API's field names are its own -- replyTo in camel case, a list for every
// recipient -- and a field it does not know is a 400 for the whole message, so
// the body is the whole transport.
func TestMeengSendsWhatTheEnvelopeSaid(t *testing.T) {
	p := &provider{answer: `{"emailId":"em_123"}`}
	server := p.serve(t)

	sent, err := testMailer(transport.Meeng{Key: "mg_test", Endpoint: server.URL}, fixedView{}, "blog@example.com").
		To("reader@example.com").CC("c@example.com").BCC("b@example.com").
		ReplyTo("help@example.com").Send(context.Background(), withHeaders{})
	if err != nil {
		t.Fatalf("the message was not accepted: %v", err)
	}

	if p.auth() != "Bearer mg_test" {
		t.Errorf("the key did not reach the provider: %q", p.auth())
	}
	if got := p.body["from"]; got != `"The Blog" <blog@example.com>` {
		t.Errorf("from is %v", got)
	}
	if got := p.body["subject"]; got != "Headers" {
		t.Errorf("subject is %v", got)
	}
	for field, want := range map[string]string{
		"to": "reader@example.com", "cc": "c@example.com",
		"bcc": "b@example.com", "replyTo": "help@example.com",
	} {
		list, _ := p.body[field].([]any)
		if len(list) != 1 || list[0] != want {
			t.Errorf("%s is %v, want [%s]", field, p.body[field], want)
		}
	}
	if got := p.body["html"]; got != "<p>the html part</p>" {
		t.Errorf("html is %v", got)
	}
	if got := p.body["text"]; got != "the text part" {
		t.Errorf("text is %v", got)
	}
	headers, _ := p.body["headers"].(map[string]any)
	if headers["X-Campaign"] != "welcome" || headers["References"] != "<one@example.com> <two@example.com>" {
		t.Errorf("headers is %v", p.body["headers"])
	}
	if sent.ID != "em_123" {
		t.Errorf("ID is %q: emailId is the only way back to the platform's record of the message", sent.ID)
	}
	if sent.Transport != "meeng" {
		t.Errorf("Transport is %q", sent.Transport)
	}
}

// TestMeengLeavesOutWhatTheAPIDoesNotName.
//
// The contract refuses any field it does not list, so a tag sent as a label
// would turn into a rejected message. Tags, metadata and an empty part stay out
// of the body.
func TestMeengLeavesOutWhatTheAPIDoesNotName(t *testing.T) {
	p := &provider{answer: `{"emailId":"em_1"}`}
	server := p.serve(t)

	if _, err := send(t, transport.Meeng{Key: "k", Endpoint: server.URL}, "reader@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tags", "metadata", "html", "headers", "cc", "bcc", "replyTo"} {
		if _, ok := p.body[field]; ok {
			t.Errorf("%s was sent: %v", field, p.body[field])
		}
	}
}

// TestMeengRefusesAnAttachmentBeforeSending.
//
// The platform keeps no files and answers 400 to a list with items. The refusal
// is made here, with a sentence that says what to do, and nothing is posted.
func TestMeengRefusesAnAttachmentBeforeSending(t *testing.T) {
	p := &provider{answer: `{"emailId":"em_1"}`}
	server := p.serve(t)

	_, err := testMailer(transport.Meeng{Key: "k", Endpoint: server.URL}, fixedView{}, "blog@example.com").
		To("reader@example.com").Send(context.Background(), withAttachment{})
	if err == nil {
		t.Fatal("a message with an attachment was sent")
	}
	if !strings.Contains(err.Error(), "attachments") || !strings.Contains(err.Error(), "link") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
	if p.headers != nil {
		t.Error("the request was made although the message was refused")
	}
	var retryable mail.ErrRetryable
	if errors.As(err, &retryable) {
		t.Error("a refused attachment was marked retryable: the job will try it forever")
	}
}

// TestMeengReadsTheThreeShapesOfAnError.
//
// The API answers {"error":{"code","message"}}, {"error":"text"} and, for a
// body that does not validate, {"success":false,"error":{"name","issues"}}. The
// reason reaches the caller in each, and only a 429 or a 5xx is retryable.
func TestMeengReadsTheThreeShapesOfAnError(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		answer  string
		want    []string
		retries bool
	}{
		{"coded", 403, `{"error":{"code":"FORBIDDEN","message":"API key is not allowed to use this domain"}}`,
			[]string{"FORBIDDEN", "not allowed to use this domain"}, false},
		{"text", 400, `{"error":"The sender domain is not verified"}`,
			[]string{"domain is not verified"}, false},
		{"validation", 400, `{"success":false,"error":{"name":"ZodError","issues":[{"path":["to"],"message":"Required"}]}}`,
			[]string{"ZodError", "to: Required"}, false},
		{"rate limited", 429, `{"error":{"code":"RATE_LIMITED","message":"Too many requests"}}`,
			[]string{"RATE_LIMITED"}, true},
		{"provider down", 502, `bad gateway`, []string{"bad gateway"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &provider{status: c.status, answer: c.answer}
			server := p.serve(t)

			_, err := send(t, transport.Meeng{Key: "k", Endpoint: server.URL}, "reader@example.com")
			if err == nil {
				t.Fatal("a failure was reported as a send")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%q is missing from %v", want, err)
				}
			}
			var retryable mail.ErrRetryable
			if got := errors.As(err, &retryable); got != c.retries {
				t.Errorf("retryable is %v, want %v: %v", got, c.retries, err)
			}
		})
	}
}

// TestMeengIsRegisteredUnderItsName: a configuration that says meeng reaches
// the transport through the same door as the others.
func TestMeengIsRegisteredUnderItsName(t *testing.T) {
	p := &provider{answer: `{"emailId":"em_9"}`}
	server := p.serve(t)

	manager := transport.Register(mail.NewMailManager(mail.ManagerConfig{
		Default: "meeng",
		Mailers: map[string]mail.MailerConfig{
			"meeng": {Transport: "meeng", Key: "mg_reg", Endpoint: server.URL},
		},
	}, fixedView{}, nil))

	mailer, err := manager.Mailer("meeng")
	if err != nil {
		t.Fatalf("the meeng transport is not registered: %v", err)
	}
	sent, err := mailer.To("reader@example.com").Send(context.Background(), bothParts{})
	if err != nil {
		t.Fatal(err)
	}
	if sent.ID != "em_9" || p.auth() != "Bearer mg_reg" {
		t.Errorf("the registered transport did not carry the config: id %q, auth %q", sent.ID, p.auth())
	}
}

// withHeaders is a mailable that sets a custom header and the References that
// thread a reply.
type withHeaders struct{}

func (withHeaders) Envelope() mail.Envelope {
	return mail.Envelope{
		From:    mail.Address{Address: "blog@example.com", Name: "The Blog"},
		Subject: "Headers",
		Tags:    []string{"welcome"},
	}
}
func (withHeaders) Content() mail.Content {
	return mail.Content{View: "mail.note", Text: "the text part"}
}
func (withHeaders) Headers() mail.Headers {
	return mail.Headers{
		References: []string{"one@example.com", "two@example.com"},
		Text:       map[string]string{"X-Campaign": "welcome"},
	}
}

// withAttachment is a mailable that carries a file.
type withAttachment struct{}

func (withAttachment) Envelope() mail.Envelope {
	return mail.Envelope{From: mail.Address{Address: "blog@example.com"}, Subject: "File"}
}
func (withAttachment) Content() mail.Content { return mail.Content{Text: "see the file"} }
func (withAttachment) Attachments() []*mail.Attachment {
	return []*mail.Attachment{mail.FromData(func() ([]byte, error) { return []byte("x"), nil }, "a.txt")}
}
