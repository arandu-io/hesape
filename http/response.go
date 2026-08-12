package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"reflect"
	"strings"

	"github.com/arandu-io/hesape/http/exceptions"
	"github.com/arandu-io/hesape/support"
)

// Arrayable answers to Illuminate\Contracts\Support\Arrayable: a value that
// knows how to present itself as an array. Response::setContent turns one into
// JSON, which is the whole reason it is named here.
type Arrayable interface {
	// ToArray answers to Arrayable::toArray.
	ToArray() map[string]any
}

// Renderable answers to Illuminate\Contracts\Support\Renderable: a value that
// draws itself, a view being the one that matters. Response::setContent calls
// it rather than casting to string, so a failure inside a template is a failure
// and not an empty page.
//
// The PHP returns a string and throws; this returns (string, error).
type Renderable interface {
	// Render answers to Renderable::render.
	Render() (string, error)
}

// JsonSerializable answers to PHP's JsonSerializable: a value that names what
// of itself is encoded. Go's encoding/json reaches the same end through
// json.Marshaler, which is checked too.
type JsonSerializable interface {
	// JsonSerialize answers to JsonSerializable::jsonSerialize.
	JsonSerialize() any
}

// Response mirrors Illuminate\Http\Response: a status, a set of headers, a set
// of cookies and a body, built before anything is written to the wire.
//
// It is a value and not a stdhttp.ResponseWriter because that is what the
// Illuminate one is: an object a controller returns, that middleware may still
// add a header to, and that is sent last. [Response.Send] is the one place it
// meets the standard library.
//
// The methods of ResponseTrait are on this type, because that is where PHP puts
// them: Status, StatusText, Content, GetOriginalContent, Header, WithHeaders,
// WithoutHeader, Cookie, WithCookie, WithoutCookie, GetCallback, WithException
// and ThrowResponse.
type Response struct {
	// status is the status code. Symfony's $statusCode.
	status int
	// headers is the ResponseHeaderBag.
	headers stdhttp.Header
	// cookies are the cookies the header bag carries. They are a slice and not
	// a map because two cookies may share a name on different paths, which is
	// how WithoutCookie expires one.
	cookies []*stdhttp.Cookie
	// content is the encoded body.
	content string
	// original is ResponseTrait::$original: what SetContent was handed, before
	// it became JSON or rendered HTML. It is what a test asserts on.
	original any
	// exception is ResponseTrait::$exception: the error that produced this
	// answer, when one did.
	exception error
	// callback is the JSONP callback. It lives here and not on JsonResponse
	// because ResponseTrait::getCallback reads it off $this, and only
	// JsonResponse ever sets it.
	callback string
	// protocolVersion is Symfony's $version. The PHP constructor sets "1.0".
	protocolVersion string
}

// NewResponse answers to Response::__construct.
//
// The PHP throws InvalidArgumentException when the content cannot be encoded,
// so this returns (*Response, error). The variadic arguments are the PHP's
// defaults: status 200 and no headers.
func NewResponse(content any, args ...any) (*Response, error) {
	r := &Response{
		status:          stdhttp.StatusOK,
		headers:         stdhttp.Header{},
		protocolVersion: "1.0",
	}
	if len(args) > 0 {
		if status, ok := args[0].(int); ok {
			r.status = status
		}
	}
	if len(args) > 1 {
		switch headers := args[1].(type) {
		case stdhttp.Header:
			for key, values := range headers {
				r.headers[stdhttp.CanonicalHeaderKey(key)] = append([]string(nil), values...)
			}
		case map[string]string:
			for key, value := range headers {
				r.headers.Set(key, value)
			}
		}
	}
	if _, err := r.SetContent(content); err != nil {
		return nil, err
	}
	return r, nil
}

// GetContent answers to Response::getContent: the encoded body, empty when
// there is none. The PHP returns string|false and transforms the false into
// "", which is what this returns.
func (r *Response) GetContent() string { return r.content }

// SetContent answers to Response::setContent.
//
// It keeps what it was handed on $original, and encodes as the PHP does: a
// value that is "JSONable" -- Arrayable, Jsonable, JsonSerializable,
// json.Marshaler, a map or a slice -- becomes JSON and sets the Content-Type;
// a Renderable is rendered; anything else is cast to a string.
//
// The PHP throws InvalidArgumentException when json_encode fails, so this
// returns (*Response, error).
func (r *Response) SetContent(content any) (*Response, error) {
	r.original = content

	switch typed := content.(type) {
	case nil:
		r.content = ""
		return r, nil
	case string:
		r.content = typed
		return r, nil
	case []byte:
		r.content = string(typed)
		return r, nil
	}

	if shouldBeJson(content) {
		r.Header("Content-Type", "application/json")
		encoded, err := morphToJson(content)
		if err != nil {
			return nil, fmt.Errorf("http: the response content could not be encoded as JSON: %w", err)
		}
		r.content = encoded
		return r, nil
	}

	if renderable, ok := content.(Renderable); ok {
		rendered, err := renderable.Render()
		if err != nil {
			return nil, fmt.Errorf("http: the response content could not be rendered: %w", err)
		}
		r.content = rendered
		return r, nil
	}

	r.content = stringify(content)
	return r, nil
}

// shouldBeJson answers to Response::shouldBeJson: whether the content is a
// value the PHP turns into JSON rather than casting to a string.
func shouldBeJson(content any) bool {
	switch content.(type) {
	case Arrayable, support.Jsonable, JsonSerializable, json.Marshaler:
		return true
	}
	// is_array($content) is the last arm of the PHP check. A map, a slice and
	// an array are what it means here; a struct is deliberately NOT included,
	// because the PHP does not encode a plain object either -- and a Renderable
	// is a struct, checked after this one.
	kind := reflect.ValueOf(content).Kind()
	return kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array
}

// morphToJson answers to Response::morphToJson.
func morphToJson(content any) (string, error) {
	switch typed := content.(type) {
	case support.Jsonable:
		return typed.ToJson()
	case Arrayable:
		encoded, err := json.Marshal(typed.ToArray())
		return string(encoded), err
	case JsonSerializable:
		encoded, err := json.Marshal(typed.JsonSerialize())
		return string(encoded), err
	}
	encoded, err := json.Marshal(content)
	return string(encoded), err
}

// Status answers to ResponseTrait::status: the status code.
func (r *Response) Status() int { return r.status }

// GetStatusCode answers to Symfony's Response::getStatusCode, which
// ResponseTrait::status calls. It is Status under the name the parent class
// declares.
func (r *Response) GetStatusCode() int { return r.status }

// SetStatusCode answers to Symfony's Response::setStatusCode, which
// Response::__construct calls.
func (r *Response) SetStatusCode(code int) *Response {
	r.status = code
	return r
}

// StatusText answers to ResponseTrait::statusText: the reason phrase.
func (r *Response) StatusText() string { return stdhttp.StatusText(r.status) }

// Content answers to ResponseTrait::content: the encoded body.
func (r *Response) Content() string { return r.GetContent() }

// GetOriginalContent answers to ResponseTrait::getOriginalContent: what
// SetContent was handed, before it became JSON or HTML. A Response wrapping a
// Response unwraps, as the PHP recurses on itself.
func (r *Response) GetOriginalContent() any {
	if nested, ok := r.original.(*Response); ok {
		return nested.GetOriginalContent()
	}
	return r.original
}

// Header answers to ResponseTrait::header: set a header, replacing what is
// there unless replace is false.
//
// The PHP takes array|string for the value; the variadic values are that array.
func (r *Response) Header(key string, values ...any) *Response {
	if r.headers == nil {
		r.headers = stdhttp.Header{}
	}
	replace := true
	list := make([]string, 0, len(values))
	for i, value := range values {
		// The PHP signature is header($key, $values, $replace = true), so a
		// trailing bool is the replace flag and never a header value.
		if flag, ok := value.(bool); ok && i == len(values)-1 && i > 0 {
			replace = flag
			continue
		}
		switch typed := value.(type) {
		case []string:
			list = append(list, typed...)
		default:
			list = append(list, stringify(value))
		}
	}
	if replace {
		r.headers.Del(key)
	}
	for _, value := range list {
		r.headers.Add(key, value)
	}
	return r
}

// WithHeaders answers to ResponseTrait::withHeaders: add several headers at
// once. The PHP takes a HeaderBag or an array; stdhttp.Header is the HeaderBag.
func (r *Response) WithHeaders(headers stdhttp.Header) *Response {
	if r.headers == nil {
		r.headers = stdhttp.Header{}
	}
	for key, values := range headers {
		r.headers.Del(key)
		for _, value := range values {
			r.headers.Add(key, value)
		}
	}
	return r
}

// WithoutHeader answers to ResponseTrait::withoutHeader: remove headers. The
// PHP takes array|string; the variadic is that array.
func (r *Response) WithoutHeader(keys ...string) *Response {
	for _, key := range keys {
		r.headers.Del(key)
	}
	return r
}

// Headers answers to Symfony's $response->headers, the ResponseHeaderBag the
// trait writes into. It is a getter because a Go field would let a caller swap
// the whole bag.
func (r *Response) Headers() stdhttp.Header {
	if r.headers == nil {
		r.headers = stdhttp.Header{}
	}
	return r.headers
}

// Cookie answers to ResponseTrait::cookie: add a cookie to the response. It is
// WithCookie under the shorter name the PHP also offers.
func (r *Response) Cookie(cookie *stdhttp.Cookie) *Response { return r.WithCookie(cookie) }

// WithCookie answers to ResponseTrait::withCookie: add a cookie.
func (r *Response) WithCookie(cookie *stdhttp.Cookie) *Response {
	if cookie != nil {
		r.cookies = append(r.cookies, cookie)
	}
	return r
}

// WithoutCookie answers to ResponseTrait::withoutCookie: expire a cookie when
// the response is sent. The PHP writes a cookie with a lifetime of -2628000
// seconds; MaxAge -1 is what tells a browser the same thing here.
//
// The variadic arguments are the PHP's optional $path and $domain.
func (r *Response) WithoutCookie(name string, args ...string) *Response {
	expired := &stdhttp.Cookie{Name: name, Value: "", MaxAge: -1}
	if len(args) > 0 {
		expired.Path = args[0]
	}
	if len(args) > 1 {
		expired.Domain = args[1]
	}
	return r.WithCookie(expired)
}

// Cookies returns the cookies this response carries, which Symfony reads off
// the ResponseHeaderBag. [Response.Send] writes them.
func (r *Response) Cookies() []*stdhttp.Cookie { return r.cookies }

// GetCallback answers to ResponseTrait::getCallback: the JSONP callback, empty
// when there is none. Only a JsonResponse ever sets one.
func (r *Response) GetCallback() string { return r.callback }

// WithException answers to ResponseTrait::withException: record the error that
// produced this answer.
func (r *Response) WithException(err error) *Response {
	r.exception = err
	return r
}

// Exception returns the error WithException recorded, which is
// ResponseTrait::$exception, a public property in the PHP.
func (r *Response) Exception() error { return r.exception }

// ThrowResponse answers to ResponseTrait::throwResponse: wrap this response in
// an HttpResponseException so the layer above sends it instead of rendering.
//
// The PHP throws; Go returns the error, and the caller returns it.
func (r *Response) ThrowResponse() error {
	return exceptions.NewHttpResponseException(r)
}

// SetProtocolVersion answers to Symfony's Response::setProtocolVersion, which
// Response::__construct calls with "1.0".
func (r *Response) SetProtocolVersion(version string) *Response {
	r.protocolVersion = version
	return r
}

// GetProtocolVersion answers to Symfony's Response::getProtocolVersion.
func (r *Response) GetProtocolVersion() string { return r.protocolVersion }

// Send answers to Symfony's Response::send: write the status, the headers, the
// cookies and the body to the wire.
//
// It is the one place this type meets stdhttp.ResponseWriter, which is the
// point of building a Response at all: everything before this is a value that
// middleware may still change.
func (r *Response) Send(w stdhttp.ResponseWriter) error {
	header := w.Header()
	for key, values := range r.headers {
		header.Del(key)
		for _, value := range values {
			header.Add(key, value)
		}
	}
	for _, cookie := range r.cookies {
		stdhttp.SetCookie(w, cookie)
	}
	status := r.status
	if status == 0 {
		status = stdhttp.StatusOK
	}
	w.WriteHeader(status)
	if r.content == "" {
		return nil
	}
	_, err := w.Write([]byte(r.content))
	return err
}

// ServeHTTP makes a Response a stdhttp.Handler, so a route may answer with one
// directly. It is Send with the arguments a handler is given.
func (r *Response) ServeHTTP(w stdhttp.ResponseWriter, _ *stdhttp.Request) { _ = r.Send(w) }

// StreamedEvent mirrors Illuminate\Http\StreamedEvent: one named event on a
// server-sent event stream.
type StreamedEvent struct {
	// Event is the name of the event.
	Event string
	// Data is the payload.
	Data any
}

// NewStreamedEvent answers to StreamedEvent::__construct.
func NewStreamedEvent(event string, data any) *StreamedEvent {
	return &StreamedEvent{Event: event, Data: data}
}

// String formats the event the way the server-sent event wire format wants it:
// an "event:" line, one "data:" line per line of payload, and a blank line.
//
// It is here and not in a writer of its own because the PHP class is two
// properties and nothing else, and the framework that reads it needs the bytes.
func (e *StreamedEvent) String() string {
	var out strings.Builder
	if e.Event != "" {
		out.WriteString("event: ")
		out.WriteString(e.Event)
		out.WriteString("\n")
	}
	payload := stringify(e.Data)
	if shouldBeJson(e.Data) {
		if encoded, err := morphToJson(e.Data); err == nil {
			payload = encoded
		}
	}
	for _, line := range strings.Split(payload, "\n") {
		out.WriteString("data: ")
		out.WriteString(line)
		out.WriteString("\n")
	}
	out.WriteString("\n")
	return out.String()
}
