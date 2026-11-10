package webhook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/session"
)

// The refusals of a relayed request.
var (
	// ErrNotRelayEndpoint is a relayed request addressed to an endpoint whose
	// dialect is not DialectRelay: an endpoint that signs its calls is not one
	// that forwards a request as it arrived.
	ErrNotRelayEndpoint = statusError{status: http.StatusConflict, text: "webhook: the endpoint does not relay requests"}
	// ErrRelayEndpoint is a message, an event or a test call for an endpoint of
	// DialectRelay, which takes only what Module.Relay records.
	ErrRelayEndpoint = statusError{status: http.StatusConflict, text: "webhook: the endpoint takes relayed requests only"}
	// ErrNoBodyDisk is a relayed body above Server.RelayInlineBytes with no
	// Server.Bodies to keep it on.
	ErrNoBodyDisk = errors.New("webhook: the body is larger than Config.Server.RelayInlineBytes and Config.Server.Bodies is not set: pass the private disk relayed bodies are kept on")
	// ErrBodySize is a relayed body that ended at another length than the one
	// RelayMessage.Size announced.
	ErrBodySize = errors.New("webhook: the body is not the length RelayMessage.Size announced")
)

// The failure codes of an attempt whose body could not be read back.
const (
	// FailureBodyUnavailable is a stored body the disk could not give at this
	// attempt; another attempt may.
	FailureBodyUnavailable = "body_unavailable"
	// FailureBodyMissing is a stored body that is not on the disk or not in the
	// row any more, which no other attempt brings back.
	FailureBodyMissing = "body_missing"
)

// bodyDirectory is where, under the tenant, the relayed bodies are kept.
const bodyDirectory = "webhook-relay/"

// orphanGrace is how old a stored body is before the clean may call it an
// orphan: long enough that a transaction of the caller's still open when the
// body was written has ended.
const orphanGrace = 24 * time.Hour

// Limits of what a relayed request carries besides its body.
const (
	maxRelayQuery       = 1024
	maxRelayPath        = 1024
	maxRelayHeaderValue = 1024
	maxRelayContentType = 255
)

// RelayMessage is a request received elsewhere, to be sent on to an endpoint
// of DialectRelay as it arrived.
type RelayMessage struct {
	// ID is the delivery's id: up to 128 letters, digits, '_', '.', ':' and
	// '-', unique across every tenant. Empty makes one.
	ID string
	// EventID is what the delivery is for; a second message with the same
	// EventID to the same endpoint answers the first delivery and sends
	// nothing. Empty means ID.
	EventID string
	// Event names the delivery in its history. Empty means DefaultRelayEvent.
	Event string
	// Method is POST or PUT. Empty means POST.
	Method string
	// Path and Query are the part of the original request's address that the
	// endpoint receives when it keeps the path: Path decoded, as
	// http.Request.URL.Path, and Query raw, without the '?'. The caller strips
	// what its own route consumed. A '..' segment is refused.
	Path  string
	Query string
	// ContentType is the Content-Type of the original request, sent as it is.
	ContentType string
	// Header is the original request's headers. Only the allowlisted ones are
	// forwarded -- Content-Encoding, Content-Language, Content-MD5, Accept,
	// Accept-Language, User-Agent, SOAPAction and Server.RelayHeaders -- and
	// never a credential or a hop of the sender's network, whatever is listed:
	// Authorization, Cookie, Host, Forwarded, X-Forwarded-*, X-Real-Ip, CF-*,
	// the Arandu and Standard Webhooks headers and every name
	// session.IsSecretField calls secret. The endpoint's own headers are laid
	// over them.
	Header http.Header
	// Body is the request body, read once, up to Policy.MaxRelayBytes. It is
	// not closed. To relay one request to several endpoints, read it once and
	// pass a bytes.Reader to each call.
	Body io.Reader
	// Size is the length the sender announced, or zero when it did not. A
	// message announcing more than the policy allows is refused before a byte
	// is read, and a body that ends at another length is refused.
	Size int64
}

// relayItem is what an outgoing relay carries beyond a JSON body.
type relayItem struct {
	method, contentType string
	size                int64
	sha256              string
	// inline is the body kept in the row; key is where it is kept otherwise.
	inline []byte
	key    string
}

// relay records one relayed request to one of the tenant's endpoints and
// queues its first attempt.
func (s *sender) relay(ctx context.Context, g auth.Grant, endpointID string, msg RelayMessage) (*Delivery, error) {
	if err := g.Check(DeliveryDispatch); err != nil {
		return nil, err
	}
	if err := auth.Writable(g); err != nil {
		return nil, err
	}
	method, contentType, err := relayRequestLine(msg)
	if err != nil {
		return nil, err
	}
	row, err := Endpoints(s.db).WhereKey(endpointID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	if row.Dialect != DialectRelay {
		return nil, ErrNotRelayEndpoint
	}
	item, err := s.target(row)
	if err != nil {
		return nil, err
	}
	if limit := item.policy.MaxRelayBytes; limit < 1 || msg.Size > limit {
		return nil, ErrPayloadTooLarge
	}
	if item.url, err = relayURL(row.URL, bool(row.KeepPath), msg.Path, msg.Query); err != nil {
		return nil, err
	}
	if err := s.forward(item.headers, msg.Header); err != nil {
		return nil, err
	}
	body, err := s.spool(ctx, g, msg, item.policy.MaxRelayBytes)
	if err != nil {
		return nil, err
	}
	body.method, body.contentType = method, contentType
	item.relay = body
	item.id, item.eventID, item.event = msg.ID, msg.EventID, msg.Event
	if strings.TrimSpace(item.event) == "" {
		item.event = DefaultRelayEvent
	}
	rows, err := s.record(ctx, g, []outgoing{item})
	if err != nil {
		s.discard(ctx, g, body.key)
		return nil, err
	}
	if rows[0].BodyKey != body.key {
		// The event had reached the endpoint already, or the id was taken: the
		// delivery that stands is the other one, and this body is unused.
		s.discard(ctx, g, body.key)
	}
	return rows[0], nil
}

// relayRequestLine validates the method and the content type of a message.
func relayRequestLine(msg RelayMessage) (method, contentType string, err error) {
	method = strings.ToUpper(strings.TrimSpace(msg.Method))
	if method == "" {
		method = http.MethodPost
	}
	if method != http.MethodPost && method != http.MethodPut {
		return "", "", fmt.Errorf("%w: a relayed request is a POST or a PUT, not %q", ErrInvalidMessage, msg.Method)
	}
	contentType = msg.ContentType
	if len(contentType) > maxRelayContentType || !headerSafe(contentType) {
		return "", "", fmt.Errorf("%w: the Content-Type cannot be sent", ErrInvalidMessage)
	}
	if msg.Body == nil || msg.Size < 0 {
		return "", "", fmt.Errorf("%w: a relayed request needs a Body and a Size of zero or more", ErrInvalidMessage)
	}
	return method, contentType, nil
}

// headerSafe reports whether a value can be written as a header: printable,
// with no line break.
func headerSafe(value string) bool {
	for i := 0; i < len(value); i++ {
		if (value[i] < ' ' && value[i] != '\t') || value[i] == 0x7f {
			return false
		}
	}
	return true
}

// relayURL is the address a relayed request is sent to: the endpoint's own,
// and, when it keeps the path, the original path and query appended to it.
func relayURL(base string, keepPath bool, path, query string) (string, error) {
	if !keepPath || (path == "" && query == "") {
		return base, nil
	}
	if len(path) > maxRelayPath || len(query) > maxRelayQuery || !headerSafe(path) || !headerSafe(query) ||
		strings.ContainsAny(query, " #") {
		return "", fmt.Errorf("%w: the path or the query cannot be sent", ErrInvalidMessage)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: the path climbs out of the endpoint's address", ErrInvalidMessage)
		}
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", ErrInvalidURL
	}
	if path != "" {
		u = u.JoinPath(path)
	}
	if query != "" {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += query
	}
	return u.String(), nil
}

// forward lays the allowlisted headers of the original request into the
// delivery's headers, without overriding one the endpoint carries itself.
func (s *sender) forward(into map[string]string, from http.Header) error {
	for name, values := range from {
		canonical := http.CanonicalHeaderKey(name)
		if !s.forwards(canonical) || len(values) == 0 {
			continue
		}
		if _, own := into[canonical]; own {
			continue
		}
		value := strings.Join(values, ", ")
		if len(value) > maxRelayHeaderValue || !headerSafe(value) {
			continue
		}
		into[canonical] = value
	}
	if len(into) > 2*MaxHeaders {
		return fmt.Errorf("%w: too many headers to forward", ErrInvalidMessage)
	}
	return nil
}

// forwards reports whether a request header travels with a relayed request.
func (s *sender) forwards(canonical string) bool {
	if forbiddenRelayHeader(canonical) || (s.custom != "" && strings.HasPrefix(strings.ToLower(canonical), strings.ToLower(s.custom)+"-")) {
		return false
	}
	return relayHeaders[canonical] || s.forwarded[canonical]
}

// relayHeaders are the request headers a relayed request carries unless the
// application adds to them with Server.RelayHeaders.
var relayHeaders = map[string]bool{
	"Content-Encoding": true, "Content-Language": true, "Content-Md5": true, "Accept": true,
	"Accept-Language": true, "User-Agent": true, "Soapaction": true,
}

// forbiddenRelayHeader reports whether a header is never forwarded: the ones
// the request itself writes, a credential, and a hop of the sender's network.
func forbiddenRelayHeader(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "content-type", "content-length", "host", "transfer-encoding", "connection", "keep-alive", "te", "trailer",
		"upgrade", "expect", "authorization", "proxy-authorization", "cookie", "set-cookie", "forwarded", "via",
		"x-real-ip", "true-client-ip", "webhook-id", "webhook-timestamp", "webhook-signature":
		return true
	}
	for _, prefix := range []string{"x-forwarded-", "cf-", "x-arandu-", "proxy-", "x-envoy-", "x-amzn-"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return session.IsSecretField(name)
}

// spool reads the message's body once, keeping it in the row when it is small
// and on the disk when it is not, and answers its size and digest.
func (s *sender) spool(ctx context.Context, g auth.Grant, msg RelayMessage, limit int64) (*relayItem, error) {
	head := make([]byte, s.inline+1)
	n, err := io.ReadFull(msg.Body, head)
	switch {
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		head = head[:n]
		if int64(n) > limit {
			return nil, ErrPayloadTooLarge
		}
		if msg.Size > 0 && msg.Size != int64(n) {
			return nil, ErrBodySize
		}
		sum := sha256.Sum256(head)
		return &relayItem{size: int64(n), sha256: hex.EncodeToString(sum[:]), inline: head}, nil
	case err != nil:
		return nil, err
	}
	if s.bodies == nil {
		return nil, ErrNoBodyDisk
	}
	digest := sha256.New()
	counted := &countingReader{r: io.LimitReader(io.MultiReader(bytes.NewReader(head), msg.Body), limit+1)}
	id, err := database.NewID()
	if err != nil {
		return nil, err
	}
	key := bodyDirectory + id
	if err := s.bodies.Put(ctx, g, key, io.TeeReader(counted, digest), "application/octet-stream"); err != nil {
		s.discard(ctx, g, key)
		return nil, err
	}
	switch {
	case counted.n > limit:
		s.discard(ctx, g, key)
		return nil, ErrPayloadTooLarge
	case msg.Size > 0 && msg.Size != counted.n:
		s.discard(ctx, g, key)
		return nil, ErrBodySize
	}
	return &relayItem{size: counted.n, sha256: hex.EncodeToString(digest.Sum(nil)), key: key}, nil
}

// countingReader counts what passes through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// discard removes a stored body that no delivery holds. It is best effort:
// whatever it leaves is found by the clean.
func (s *sender) discard(ctx context.Context, g auth.Grant, key string) {
	if key == "" || s.bodies == nil {
		return
	}
	_ = s.bodies.Delete(context.WithoutCancel(ctx), g, key)
}

// relayBody is how a claimed relayed delivery's body is read back: the bytes
// in its row, or a reader on the disk, opened again at every attempt.
func (s *sender) relayBody(g auth.Grant, d *Delivery) (inline []byte, open func(context.Context) (io.ReadCloser, error), refused *verdict) {
	if d.BodyKey != "" {
		bodies, key := s.bodies, d.BodyKey
		return nil, func(ctx context.Context) (io.ReadCloser, error) {
			if bodies == nil {
				return nil, errors.New("webhook: Config.Server.Bodies is not set, and this delivery's body is on it")
			}
			file, err := bodies.Get(ctx, g, key)
			if err != nil {
				return nil, err
			}
			return file.Body, nil
		}, nil
	}
	if d.BodyEncoding == BodyBase64 {
		raw, err := base64.StdEncoding.DecodeString(d.Body)
		if err != nil {
			return nil, nil, &verdict{Code: FailureBodyMissing, Permanent: true}
		}
		return raw, nil, nil
	}
	return []byte(d.Body), nil, nil
}

// bodyVerdict is what an attempt comes to when its stored body cannot be read.
func bodyVerdict(err error) verdict {
	if errors.Is(err, filesystem.ErrNotFound) {
		return verdict{Code: FailureBodyMissing, Permanent: true}
	}
	return verdict{Code: FailureBodyUnavailable}
}

// relayHeader writes the headers of a relayed attempt beyond the forwarded and
// the endpoint's own: the original Content-Type and, unless the original
// request carried one, the policy's User-Agent. It signs nothing.
func relayHeader(h http.Header, s shot, userAgent string) {
	if s.contentType != "" {
		h.Set("Content-Type", s.contentType)
	}
	if userAgent != "" && h.Get("User-Agent") == "" {
		h.Set("User-Agent", userAgent)
	}
}

// cleanBodies removes the stored bodies of the settled deliveries older than
// the cutoff, which the clean then removes, and the bodies no delivery holds.
// A body that cannot be removed stops the clean before its delivery goes, so
// the row that names it is still there at the next run.
func (s *sender) cleanBodies(ctx context.Context, g auth.Grant, cutoff time.Time) error {
	if s.bodies == nil {
		return nil
	}
	err := Deliveries(s.db).Where("created_at", "<", cutoff).WhereIn("status", []any{DeliveryDelivered, DeliveryFailed}).
		Where("body_key", "<>", "").OrderBy("id").
		Chunk(ctx, g, 200, func(rows DeliveryCollection, _ int) (bool, error) {
			for _, row := range rows {
				if err := s.bodies.Delete(ctx, g, row.BodyKey); err != nil {
					return false, err
				}
			}
			return true, nil
		})
	if err != nil {
		return err
	}
	return s.sweepOrphans(ctx, g)
}

// sweepOrphans removes the bodies a relay wrote and no delivery was ever
// recorded for: the transaction of the caller that relayed them rolled back.
func (s *sender) sweepOrphans(ctx context.Context, g auth.Grant) error {
	keys, err := s.bodies.AllFiles(ctx, g, bodyDirectory)
	if err != nil {
		return err
	}
	for _, key := range keys {
		info, err := s.bodies.Stat(ctx, g, key)
		if err != nil {
			if errors.Is(err, filesystem.ErrNotFound) {
				continue
			}
			return err
		}
		// A driver that does not say when a body was written leaves it alone:
		// its age is not known, and a body of a transaction still open is
		// not an orphan.
		if info.ModifiedAt.IsZero() || s.clock().Sub(info.ModifiedAt) < orphanGrace {
			continue
		}
		held, err := Deliveries(s.db).Where("body_key", "=", key).First(ctx, g)
		if err != nil {
			return err
		}
		if held != nil {
			continue
		}
		if err := s.bodies.Delete(ctx, g, key); err != nil {
			return err
		}
	}
	return nil
}
