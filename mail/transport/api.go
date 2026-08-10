package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arandu-io/hesape/mail"
)

// Resend sends through resend.com.
//
// It is the default recommendation for an application that has outgrown the log
// transport: a domain, a DNS record and an API key, and no server to run.
type Resend struct {
	// Key is the API key, `re_...`. It comes from the environment and never from
	// a literal -- a key in source is a key in every clone of the repository.
	Key string

	// Endpoint overrides the API, for a test. Empty is resend.com.
	Endpoint string

	// Timeout bounds the request. Without one a hung provider holds the request
	// that triggered it for as long as the provider likes.
	Timeout time.Duration

	// Client is the HTTP client. Empty builds one with Timeout.
	Client *http.Client
}

// Name identifies the transport in a log line.
func (Resend) Name() string { return "resend" }

// Send posts the message.
func (t Resend) Send(ctx context.Context, m mail.Message) (mail.Sent, error) {
	body := map[string]any{
		"from":    m.From.String(),
		"to":      addressList(m.To),
		"subject": m.Subject,
	}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if m.Text != "" {
		body["text"] = m.Text
	}
	if len(m.CC) > 0 {
		body["cc"] = addressList(m.CC)
	}
	if len(m.BCC) > 0 {
		body["bcc"] = addressList(m.BCC)
	}
	if len(m.ReplyTo) > 0 {
		body["reply_to"] = addressList(m.ReplyTo)
	}
	// Resend's tags are key/value and reject anything outside [A-Za-z0-9_-], so
	// an envelope tag becomes the value of a "tag" key rather than being sent
	// raw and rejected as a whole message.
	for _, tag := range m.Tags {
		body["tags"] = append(asSlice(body["tags"]), map[string]string{"name": "tag", "value": slug(tag)})
	}

	answer, err := post(ctx, t.Client, t.Timeout,
		endpointOr(t.Endpoint, "https://api.resend.com/emails"),
		map[string]string{"Authorization": "Bearer " + t.Key},
		body, resendError)
	if err != nil {
		return mail.Sent{}, err
	}

	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(answer.body, &out)
	return mail.Sent{ID: out.ID, Transport: t.Name()}, nil
}

// SendGrid sends through sendgrid.com.
//
// The second provider rather than the only one, because a transport with one
// implementation is an interface nobody has proved is an interface.
type SendGrid struct {
	// Key is the API key, `SG....`.
	Key string

	// Endpoint overrides the API, for a test. Empty is sendgrid.com.
	Endpoint string

	// Timeout bounds the request.
	Timeout time.Duration

	// Client is the HTTP client. Empty builds one with Timeout.
	Client *http.Client
}

// Name identifies the transport in a log line.
func (SendGrid) Name() string { return "sendgrid" }

// Send posts the message.
func (t SendGrid) Send(ctx context.Context, m mail.Message) (mail.Sent, error) {
	// One personalization holding every recipient. SendGrid's other reading of
	// this field -- one personalization per recipient -- is a different feature
	// (a separate message each, with its own substitutions), and using it here
	// would turn one message to three people into three messages.
	to := make([]map[string]string, 0, len(m.To))
	for _, a := range m.To {
		to = append(to, sendgridAddress(a))
	}
	person := map[string]any{"to": to}
	if len(m.CC) > 0 {
		person["cc"] = sendgridAddresses(m.CC)
	}
	if len(m.BCC) > 0 {
		person["bcc"] = sendgridAddresses(m.BCC)
	}

	// Text before HTML. SendGrid sends the parts in the order they arrive, and
	// MIME says the richest alternative comes last -- reversed, every client
	// shows the plain-text version of a message that has HTML.
	content := make([]map[string]string, 0, 2)
	if m.Text != "" {
		content = append(content, map[string]string{"type": "text/plain", "value": m.Text})
	}
	if m.HTML != "" {
		content = append(content, map[string]string{"type": "text/html", "value": m.HTML})
	}

	body := map[string]any{
		"personalizations": []any{person},
		"from":             sendgridAddress(m.From),
		"subject":          m.Subject,
		"content":          content,
	}
	if len(m.ReplyTo) > 0 {
		body["reply_to"] = sendgridAddress(m.ReplyTo[0])
	}
	if len(m.Tags) > 0 {
		body["categories"] = m.Tags
	}

	answer, err := post(ctx, t.Client, t.Timeout,
		endpointOr(t.Endpoint, "https://api.sendgrid.com/v3/mail/send"),
		map[string]string{"Authorization": "Bearer " + t.Key},
		body, sendgridError)
	if err != nil {
		return mail.Sent{}, err
	}

	// SendGrid answers 202 with an empty body and puts the identifier in a
	// header, which is the one place a caller would never think to look.
	return mail.Sent{ID: answer.header.Get("X-Message-Id"), Transport: t.Name()}, nil
}

// Postmark sends through postmarkapp.com.
//
// The third provider, and the one an application moves to when deliverability
// is the thing being bought rather than the API. Same cost as the other two:
// one POST, no SDK.
type Postmark struct {
	// Token is the server token. It is a per-server credential rather than an
	// account one, so the token that sends transactional mail cannot be used to
	// send anything else.
	Token string

	// Stream is the message stream to send on, `outbound` unless the server was
	// configured otherwise. Empty lets Postmark choose the default stream, which
	// is what a new server has.
	//
	// It matters because Postmark refuses a broadcast on a transactional stream
	// and the other way around, and the refusal names the stream -- so getting
	// this wrong is at least loud.
	Stream string

	// Endpoint overrides the API, for a test. Empty is postmarkapp.com.
	Endpoint string

	// Timeout bounds the request.
	Timeout time.Duration

	// Client is the HTTP client. Empty builds one with Timeout.
	Client *http.Client
}

// Name identifies the transport in a log line.
func (Postmark) Name() string { return "postmark" }

// Send posts the message.
func (t Postmark) Send(ctx context.Context, m mail.Message) (mail.Sent, error) {
	body := map[string]any{
		"From":    m.From.String(),
		"To":      strings.Join(addressList(m.To), ", "),
		"Subject": m.Subject,
	}
	if m.HTML != "" {
		body["HtmlBody"] = m.HTML
	}
	if m.Text != "" {
		body["TextBody"] = m.Text
	}
	if len(m.CC) > 0 {
		body["Cc"] = strings.Join(addressList(m.CC), ", ")
	}
	if len(m.BCC) > 0 {
		body["Bcc"] = strings.Join(addressList(m.BCC), ", ")
	}
	if len(m.ReplyTo) > 0 {
		body["ReplyTo"] = strings.Join(addressList(m.ReplyTo), ", ")
	}
	// Postmark's Tag is one string, not a list: a message belongs to a single
	// category in its dashboard. The first tag wins rather than the tags being
	// joined, because a joined tag groups nothing -- every combination becomes
	// its own row.
	if len(m.Tags) > 0 {
		body["Tag"] = m.Tags[0]
	}
	if t.Stream != "" {
		body["MessageStream"] = t.Stream
	}

	answer, err := post(ctx, t.Client, t.Timeout,
		endpointOr(t.Endpoint, "https://api.postmarkapp.com/email"),
		map[string]string{
			"X-Postmark-Server-Token": t.Token,
			"Accept":                  "application/json",
		},
		body, postmarkError)
	if err != nil {
		return mail.Sent{}, err
	}

	var out struct {
		MessageID string `json:"MessageID"`
	}
	_ = json.Unmarshal(answer.body, &out)
	return mail.Sent{ID: out.MessageID, Transport: t.Name()}, nil
}

// answer is what came back from a provider that accepted the message, in the
// two pieces a transport reads afterwards: the header SendGrid keeps its
// identifier in, and the body the other two keep theirs in.
type answer struct {
	header http.Header
	body   []byte
}

// post is the whole of the provider transports that is not the body.
func post(ctx context.Context, client *http.Client, timeout time.Duration,
	url string, headers map[string]string, body any,
	describe func(status int, body []byte) string) (answer, error) {

	encoded, err := json.Marshal(body)
	if err != nil {
		return answer{}, fmt.Errorf("mail: encoding the message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return answer{}, fmt.Errorf("mail: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if client == nil {
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		// The request never got an answer: a DNS failure, a refused connection,
		// a timeout. Every one of those is worth retrying.
		return answer{}, mail.Retryable(fmt.Errorf("mail: %w", err))
	}
	defer resp.Body.Close()

	// Bounded, because an error body is the one place a provider has no reason
	// to be large and every reason to be attacker-influenced.
	read, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	out := answer{header: resp.Header, body: read}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return out, nil
	}
	failure := fmt.Errorf("mail: the provider answered %d: %s", resp.StatusCode, describe(resp.StatusCode, read))
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return answer{}, mail.Retryable(failure)
	}
	return answer{}, failure
}

// resendError reads {"statusCode":..,"message":..,"name":..}.
func resendError(_ int, body []byte) string {
	var out struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Message == "" {
		return truncate(string(body))
	}
	if out.Name != "" {
		return out.Name + ": " + out.Message
	}
	return out.Message
}

// sendgridError reads {"errors":[{"message":..,"field":..}]}.
func sendgridError(_ int, body []byte) string {
	var out struct {
		Errors []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Errors) == 0 {
		return truncate(string(body))
	}
	parts := make([]string, 0, len(out.Errors))
	for _, e := range out.Errors {
		if e.Field != "" {
			parts = append(parts, e.Field+": "+e.Message)
			continue
		}
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

// postmarkError reads {"ErrorCode":..,"Message":..}. The code is carried into
// the message because Postmark documents each one by number, and the number is
// what somebody searches for.
func postmarkError(_ int, body []byte) string {
	var out struct {
		ErrorCode int    `json:"ErrorCode"`
		Message   string `json:"Message"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Message == "" {
		return truncate(string(body))
	}
	if out.ErrorCode != 0 {
		return fmt.Sprintf("%d: %s", out.ErrorCode, out.Message)
	}
	return out.Message
}

func addressList(list []mail.Address) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.String())
	}
	return out
}

func sendgridAddress(a mail.Address) map[string]string {
	out := map[string]string{"email": a.Email}
	if a.Name != "" {
		out["name"] = a.Name
	}
	return out
}

func sendgridAddresses(list []mail.Address) []map[string]string {
	out := make([]map[string]string, 0, len(list))
	for _, a := range list {
		out = append(out, sendgridAddress(a))
	}
	return out
}

func asSlice(v any) []map[string]string {
	if out, ok := v.([]map[string]string); ok {
		return out
	}
	return nil
}

// slug reduces a tag to what a provider accepts in one.
func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no answer body"
	}
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

func endpointOr(configured, fallback string) string {
	if configured != "" {
		return configured
	}
	return fallback
}

var (
	_ mail.Transport = Resend{}
	_ mail.Transport = SendGrid{}
	_ mail.Transport = Postmark{}
)
