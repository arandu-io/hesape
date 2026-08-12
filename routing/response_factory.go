package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"unicode"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing/exceptions"
)

// ResponseFactory mirrors Illuminate\Routing\ResponseFactory: the one place a
// handler asks for an answer of a given shape -- a body, a view, JSON, a
// stream, a file, a redirect -- instead of assembling one by hand.
//
// It holds the two collaborators the PHP constructor takes: a [ViewRenderer],
// which is this package's seam onto the view layer, and a [Redirector]. Every
// method named Redirect* is a delegation to that redirector and nothing else,
// exactly as in the PHP -- there is one redirect mechanism in this package, not
// two (RULE 9), which is why those methods answer with the [Redirect] the
// Redirector builds rather than with an hhttp.RedirectResponse.
//
// The names are the Laravel names (ADR 0044). The single spelling liberty is
// acronym casing, which Go writes upper: ResponseFactory::json is [JSON] and
// ResponseFactory::jsonp is [JSONP].
type ResponseFactory struct {
	// view answers to ResponseFactory::$view.
	view ViewRenderer
	// redirector answers to ResponseFactory::$redirector.
	redirector *Redirector
}

// NewResponseFactory answers to ResponseFactory::__construct.
//
// Either collaborator may be nil, and the method that needs the missing one
// says so rather than panicking: a project that never returns a view has no
// renderer to give.
func NewResponseFactory(view ViewRenderer, redirector *Redirector) *ResponseFactory {
	return &ResponseFactory{view: view, redirector: redirector}
}

// Make answers to ResponseFactory::make: a response around a body.
//
// The PHP defaults are status 200 and no headers; a status of 0 means that
// default here, as it does everywhere else in this package. The PHP throws
// InvalidArgumentException when the content cannot be encoded, so this returns
// (*hhttp.Response, error).
func (f *ResponseFactory) Make(content any, status int, headers http.Header) (*hhttp.Response, error) {
	return hhttp.NewResponse(content, responseFactoryStatus(status, http.StatusOK), responseFactoryHeaders(headers))
}

// NoContent answers to ResponseFactory::noContent: an empty body under a status
// that says there is nothing to read. A status of 0 means the PHP default, 204.
func (f *ResponseFactory) NoContent(status int, headers http.Header) (*hhttp.Response, error) {
	return f.Make("", responseFactoryStatus(status, http.StatusNoContent), headers)
}

// View answers to ResponseFactory::view: render a named view and answer with
// what it drew.
//
// It goes through [ViewRenderer], which is the seam this package already uses
// to reach the view layer without importing it.
//
// The PHP accepts string|array for the view and calls ViewFactory::first on the
// array, answering with the first of several names that exists. Go has no such
// union and the seam has no First, so this takes the one name; a caller that
// wants a fallback chooses it before calling.
//
// The PHP throws when the template is missing or fails, so this returns
// (*hhttp.Response, error).
func (f *ResponseFactory) View(view string, data any, status int, headers http.Header) (*hhttp.Response, error) {
	if f.view == nil {
		return nil, errors.New("routing: expected a view renderer on the response factory, got none")
	}
	status = responseFactoryStatus(status, http.StatusOK)
	headers = responseFactoryHeaders(headers)

	var body bytes.Buffer
	if err := f.view.Render(&body, view, data, status, headers); err != nil {
		return nil, fmt.Errorf("routing: the view %q could not be rendered: %w", view, err)
	}
	return f.Make(body.String(), status, headers)
}

// JSON answers to ResponseFactory::json. The name is the PHP one with the
// acronym in Go casing.
//
// The options word is the PHP JSON_* flags, whose constants live in hhttp; the
// PHP default is JsonResponse::DEFAULT_ENCODING_OPTIONS, four hex-escaping
// flags encoding/json has no counterpart for, so 0 is the default here.
//
// The PHP throws InvalidArgumentException when encoding fails, so this returns
// (*hhttp.JsonResponse, error).
func (f *ResponseFactory) JSON(data any, status int, headers http.Header, options int) (*hhttp.JsonResponse, error) {
	return hhttp.NewJsonResponse(data, responseFactoryStatus(status, http.StatusOK), responseFactoryHeaders(headers), options)
}

// JSONP answers to ResponseFactory::jsonp: [JSON] with the payload wrapped in a
// call to the named callback, which turns the Content-Type into text/javascript.
// The name is the PHP one with the acronym in Go casing.
func (f *ResponseFactory) JSONP(callback string, data any, status int, headers http.Header, options int) (*hhttp.JsonResponse, error) {
	response, err := f.JSON(data, status, headers, options)
	if err != nil {
		return nil, err
	}
	return response.SetCallback(callback), nil
}

// Stream answers to ResponseFactory::stream: a response whose body is written
// as it is produced, rather than assembled and then sent.
//
// The PHP callback echoes and the output buffer is flushed for it; here the
// callback is handed an io.Writer that flushes after every write, so the bytes
// reach the client at the point the PHP's flush() would have sent them. It also
// receives a context, because a stream is I/O that outlives the call that
// started it and the client may hang up mid-body.
func (f *ResponseFactory) Stream(callback StreamCallback, status int, headers http.Header) *StreamedResponse {
	return &StreamedResponse{
		Callback: callback,
		Status:   responseFactoryStatus(status, http.StatusOK),
		Headers:  responseFactoryHeaders(headers),
	}
}

// StreamJSON answers to ResponseFactory::streamJson: encode the value straight
// onto the wire instead of building the whole document in memory first.
//
// The PHP answers with Symfony's StreamedJsonResponse; here it is a
// [StreamedResponse] whose callback is the encoder, because a second streaming
// response type would be a second way to stream (RULE 9).
//
// encodingOptions is the same flag word [JSON] takes.
func (f *ResponseFactory) StreamJSON(data any, status int, headers http.Header, encodingOptions int) *StreamedResponse {
	response := f.Stream(func(_ context.Context, w io.Writer) error {
		return responseFactoryEncodeJSON(w, data, encodingOptions)
	}, status, headers)

	if response.Headers.Get("Content-Type") == "" {
		response.Headers.Set("Content-Type", "application/json")
	}
	return response
}

// EventStream answers to ResponseFactory::eventStream: a server-sent event
// stream, with the three headers that stop a proxy or a browser from buffering
// it.
//
// The PHP callback is a generator that yields a message at a time; the Go
// counterpart is an iter.Seq, which is the same thing -- a producer that is
// pulled, not a slice built in advance. A message that is an
// *hhttp.StreamedEvent names its own event; anything else is sent under
// "update", and a value that is neither a string nor a number is encoded as
// JSON, which is what Js::encode does there.
//
// endStreamWith is the PHP's optional argument, whose default is "</stream>":
// omit it for that default, and pass nil to close the stream without a final
// event. Passing an *hhttp.StreamedEvent names the closing event.
//
// The PHP checks connection_aborted() before each message; here the iteration
// stops when the request context is done, which is the same question asked of
// the standard library.
func (f *ResponseFactory) EventStream(callback iter.Seq[any], headers http.Header, endStreamWith ...any) *StreamedResponse {
	end := any(responseFactoryDefaultEndStream)
	if len(endStreamWith) > 0 {
		end = endStreamWith[0]
	}

	merged := responseFactoryHeaders(headers)
	merged.Set("Content-Type", "text/event-stream")
	merged.Set("Cache-Control", "no-cache")
	merged.Set("X-Accel-Buffering", "no")

	return f.Stream(func(ctx context.Context, w io.Writer) error {
		var failure error
		if callback != nil {
			callback(func(message any) bool {
				if ctx.Err() != nil {
					return false
				}
				if _, err := io.WriteString(w, responseFactoryEvent(message)); err != nil {
					failure = err
					return false
				}
				return true
			})
		}
		if failure != nil {
			return failure
		}
		// The PHP writes the closing event outside the loop, so it is written
		// even when the loop stopped early.
		if responseFactoryFilled(end) {
			if _, err := io.WriteString(w, responseFactoryEvent(end)); err != nil {
				return err
			}
		}
		return nil
	}, http.StatusOK, merged)
}

// StreamDownload answers to ResponseFactory::streamDownload: a [Stream] the
// browser saves instead of showing, because of the Content-Disposition header.
//
// The PHP wraps whatever the callback throws in a StreamedResponseException,
// since by then the status and the headers are already gone and there is no
// error page left to send; this wraps the returned error in
// exceptions.StreamedResponseError for the same reason.
//
// An empty name leaves the header off, as a null does in the PHP. An empty
// disposition means the PHP default, "attachment".
func (f *ResponseFactory) StreamDownload(callback StreamCallback, name string, headers http.Header, disposition string) *StreamedResponse {
	response := f.Stream(callback, http.StatusOK, headers)
	response.wrapErrors = true

	if name != "" {
		response.Headers.Set("Content-Disposition", responseFactoryMakeDisposition(
			responseFactoryDisposition(disposition),
			name,
			responseFactoryFallbackName(name),
		))
	}
	return response
}

// Download answers to ResponseFactory::download: send a file from disk under a
// Content-Disposition that makes the browser save it.
//
// The PHP takes SplFileInfo|string; this takes the path, and the file is opened
// when the response is sent rather than read into memory when it is built --
// [BinaryFileResponse] serves it through http.ServeContent, so a range request
// and a conditional request are answered the way the standard library answers
// them.
//
// An empty name means the file's own base name, as a null does in the PHP. An
// empty disposition means the PHP default, "attachment".
func (f *ResponseFactory) Download(path, name string, headers http.Header, disposition string) *BinaryFileResponse {
	response := &BinaryFileResponse{Path: path, Headers: responseFactoryHeaders(headers)}
	if name == "" {
		name = responseFactoryBaseName(path)
	}
	return response.SetContentDisposition(responseFactoryDisposition(disposition), name, responseFactoryFallbackName(name))
}

// File answers to ResponseFactory::file: send the raw contents of a file, with
// no Content-Disposition, so a PDF or an image is shown rather than saved.
//
// The PHP takes SplFileInfo|string; this takes the path, for the reason
// [Download] does.
func (f *ResponseFactory) File(path string, headers http.Header) *BinaryFileResponse {
	return &BinaryFileResponse{Path: path, Headers: responseFactoryHeaders(headers)}
}

// RedirectTo answers to ResponseFactory::redirectTo. It is Redirector.To, which
// is what the PHP delegates to.
func (f *ResponseFactory) RedirectTo(path string, status int, headers http.Header, secure *bool) (*Redirect, error) {
	if f.redirector == nil {
		return nil, responseFactoryNoRedirector()
	}
	return f.redirector.To(path, responseFactoryStatus(status, http.StatusFound), headers, secure), nil
}

// RedirectToRoute answers to ResponseFactory::redirectToRoute. It is
// Redirector.Route.
//
// The PHP takes BackedEnum|string for the route; the enum case is a PHP
// language feature, and the name it carries is the string this takes.
func (f *ResponseFactory) RedirectToRoute(route string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	if f.redirector == nil {
		return nil, responseFactoryNoRedirector()
	}
	return f.redirector.Route(route, parameters, responseFactoryStatus(status, http.StatusFound), headers)
}

// RedirectToAction answers to ResponseFactory::redirectToAction. It is
// Redirector.Action.
func (f *ResponseFactory) RedirectToAction(action string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	if f.redirector == nil {
		return nil, responseFactoryNoRedirector()
	}
	return f.redirector.Action(action, parameters, responseFactoryStatus(status, http.StatusFound), headers)
}

// RedirectGuest answers to ResponseFactory::redirectGuest. It is
// Redirector.Guest: the redirect that remembers where the visitor was going, so
// a sign-in can send them back there.
func (f *ResponseFactory) RedirectGuest(path string, status int, headers http.Header, secure *bool) (*Redirect, error) {
	if f.redirector == nil {
		return nil, responseFactoryNoRedirector()
	}
	return f.redirector.Guest(path, responseFactoryStatus(status, http.StatusFound), headers, secure), nil
}

// RedirectToIntended answers to ResponseFactory::redirectToIntended. It is
// Redirector.Intended: the other half of [RedirectGuest].
//
// An empty default means the PHP default, "/".
func (f *ResponseFactory) RedirectToIntended(def string, status int, headers http.Header, secure *bool) (*Redirect, error) {
	if f.redirector == nil {
		return nil, responseFactoryNoRedirector()
	}
	if def == "" {
		def = "/"
	}
	return f.redirector.Intended(def, responseFactoryStatus(status, http.StatusFound), headers, secure), nil
}

// StreamCallback is the $callback ResponseFactory::stream, streamDownload and
// eventStream take. The PHP echoes into the output buffer; this writes to w,
// which flushes after every write.
//
// The context is the request's: it is done when the client hangs up, which is
// the question connection_aborted() answers there.
type StreamCallback func(ctx context.Context, w io.Writer) error

// StreamedResponse mirrors Symfony's StreamedResponse, which
// ResponseFactory::stream, streamJson, eventStream and streamDownload all
// answer with: a status, a set of headers and a body that is produced while it
// is being sent.
//
// The fields are public because the whole point of returning a response instead
// of writing one is that a caller may still add a header to it.
type StreamedResponse struct {
	// Callback writes the body.
	Callback StreamCallback
	// Status is the status code, 200 when zero.
	Status int
	// Headers are sent before the first byte of the body.
	Headers http.Header
	// wrapErrors reports whether a failure from the callback is wrapped in an
	// exceptions.StreamedResponseError. Only StreamDownload sets it, which is
	// the only PHP method that wraps.
	wrapErrors bool
}

// SendContent answers to Symfony's StreamedResponse::sendContent: write the
// headers, the status and then the body, flushing as it goes.
//
// It returns what the callback returned, which ServeHTTP cannot: by the time
// the callback fails the status line is already sent, so there is nowhere left
// to report it to the client, and a caller that wants to log it needs this.
func (r *StreamedResponse) SendContent(w http.ResponseWriter, req *http.Request) error {
	if w == nil {
		return errors.New("routing: expected a response writer to stream into, got none")
	}
	responseFactoryApplyHeaders(w, r.Headers)

	status := responseFactoryStatus(r.Status, http.StatusOK)
	w.WriteHeader(status)

	if r.Callback == nil {
		return nil
	}

	ctx := context.Background()
	if req != nil {
		ctx = req.Context()
	}

	out := &responseFactoryFlushWriter{w: w}
	if flusher, ok := w.(http.Flusher); ok {
		out.flusher = flusher
	}
	// The headers reach the client before the first message, which is what an
	// event stream needs to start being read at all.
	out.Flush()

	if err := r.Callback(ctx, out); err != nil {
		if r.wrapErrors {
			return &exceptions.StreamedResponseError{Err: err}
		}
		return err
	}
	return nil
}

// ServeHTTP makes a StreamedResponse an http.Handler, so a route may answer
// with one directly. It is SendContent with the arguments a handler is given,
// mirroring how hhttp.Response does it.
func (r *StreamedResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	_ = r.SendContent(w, req)
}

// BinaryFileResponse mirrors Symfony's BinaryFileResponse, which
// ResponseFactory::download and file answer with: a path on disk plus the
// headers to send it under.
//
// The bytes are read when the response is sent, never before, so a large file
// costs a descriptor and not memory.
type BinaryFileResponse struct {
	// Path is the file to send.
	Path string
	// Headers are sent alongside the ones http.ServeContent derives from the
	// file itself.
	Headers http.Header
}

// SetContentDisposition answers to BinaryFileResponse::setContentDisposition:
// say whether the browser saves the file or shows it, and under what name.
//
// The fallback is the ASCII name a client that does not understand RFC 5987
// falls back to, which is what ResponseFactory::fallbackName produces.
func (r *BinaryFileResponse) SetContentDisposition(disposition, name, fallback string) *BinaryFileResponse {
	if r.Headers == nil {
		r.Headers = http.Header{}
	}
	r.Headers.Set("Content-Disposition", responseFactoryMakeDisposition(disposition, name, fallback))
	return r
}

// SendContent answers to Symfony's BinaryFileResponse::sendContent: open the
// file and hand it to http.ServeContent, which picks the status -- 200, 206 for
// a range request, 304 for a conditional one -- and streams the bytes.
func (r *BinaryFileResponse) SendContent(w http.ResponseWriter, req *http.Request) error {
	if w == nil {
		return errors.New("routing: expected a response writer to send a file into, got none")
	}
	if req == nil {
		return errors.New("routing: expected a request to serve a file against, got none")
	}

	file, err := os.Open(r.Path)
	if err != nil {
		return fmt.Errorf("routing: the file %q could not be opened to be sent: %w", r.Path, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("routing: the file %q could not be inspected to be sent: %w", r.Path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("routing: expected a file to send, got the directory %q", r.Path)
	}

	responseFactoryApplyHeaders(w, r.Headers)
	http.ServeContent(w, req, info.Name(), info.ModTime(), file)
	return nil
}

// ServeHTTP makes a BinaryFileResponse an http.Handler. A file that cannot be
// opened is a 404, which is what Symfony's FileException becomes once the
// exception handler has had it.
func (r *BinaryFileResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := r.SendContent(w, req); err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// responseFactoryDefaultEndStream is the PHP default of the $endStreamWith
// argument of ResponseFactory::eventStream.
const responseFactoryDefaultEndStream = "</stream>"

// responseFactoryDefaultEvent is the event name eventStream sends a message
// under when the message did not name one.
const responseFactoryDefaultEvent = "update"

// responseFactoryNoRedirector is the error every Redirect* method answers with
// when the factory was built without one.
func responseFactoryNoRedirector() error {
	return errors.New("routing: expected a redirector on the response factory, got none")
}

// responseFactoryStatus applies a PHP default argument: zero means the default.
func responseFactoryStatus(status, def int) int {
	if status == 0 {
		return def
	}
	return status
}

// responseFactoryDisposition applies the PHP default of $disposition.
func responseFactoryDisposition(disposition string) string {
	if disposition == "" {
		return "attachment"
	}
	return disposition
}

// responseFactoryHeaders copies the caller's headers into a bag this package
// owns, canonicalised and never nil, so that setting Content-Type on a stream
// does not reach back into the map the caller still holds.
func responseFactoryHeaders(headers http.Header) http.Header {
	out := http.Header{}
	for key, values := range headers {
		out[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return out
}

// responseFactoryApplyHeaders writes a header bag onto a response writer,
// replacing rather than appending, which is what Symfony's send() does.
func responseFactoryApplyHeaders(w http.ResponseWriter, headers http.Header) {
	target := w.Header()
	for key, values := range headers {
		target.Del(key)
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

// responseFactoryFlushWriter is the io.Writer a StreamCallback is handed: every
// write reaches the client, which is what the PHP's ob_flush() plus flush()
// pair does after each event.
type responseFactoryFlushWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (fw *responseFactoryFlushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.Flush()
	return n, err
}

// Flush makes the writer an http.Flusher too, so a callback that batches its
// own writes can still say when a message is complete.
func (fw *responseFactoryFlushWriter) Flush() {
	if fw.flusher != nil {
		fw.flusher.Flush()
	}
}

// responseFactoryEvent frames one server-sent event, which is what the three
// echo lines of ResponseFactory::eventStream produce.
//
// hhttp.StreamedEvent already knows the wire format and already encodes a
// payload that is neither a string nor a number as JSON, so this only supplies
// the default event name the PHP supplies.
func responseFactoryEvent(message any) string {
	if event, ok := message.(*hhttp.StreamedEvent); ok && event != nil {
		name := event.Event
		if name == "" {
			name = responseFactoryDefaultEvent
		}
		// Copied rather than mutated: the caller still owns the event it
		// yielded.
		return (&hhttp.StreamedEvent{Event: name, Data: event.Data}).String()
	}
	return (&hhttp.StreamedEvent{Event: responseFactoryDefaultEvent, Data: message}).String()
}

// responseFactoryFilled answers to the filled() helper the PHP guards the
// closing event with: nil and the empty string are not filled.
func responseFactoryFilled(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case *hhttp.StreamedEvent:
		return typed != nil
	}
	return true
}

// responseFactoryEncodeJSON writes the value as JSON, honouring the flags of
// the options word that encoding/json can express -- the same two hhttp's
// JsonResponse honours, for the same reason.
func responseFactoryEncodeJSON(w io.Writer, data any, options int) error {
	encoder := json.NewEncoder(w)
	if options&(hhttp.JSONUnescapedSlashes|hhttp.JSONUnescapedUnicode) != 0 {
		encoder.SetEscapeHTML(false)
	}
	if options&hhttp.JSONPrettyPrint != 0 {
		encoder.SetIndent("", "    ")
	}
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("routing: the streamed payload could not be encoded as JSON: %w", err)
	}
	return nil
}

// responseFactoryBaseName is the file's own name, which Symfony's
// BinaryFileResponse uses when download() was given no name.
func responseFactoryBaseName(path string) string {
	// Deliberately not filepath.Base: the name goes into an HTTP header, where
	// the separator is "/" whatever the server's operating system thinks.
	trimmed := strings.TrimRight(path, "/")
	if index := strings.LastIndexAny(trimmed, `/\`); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

// responseFactoryFallbackName answers to ResponseFactory::fallbackName: the
// ASCII name a client that does not read RFC 5987 falls back to.
//
// The PHP is str_replace('%', ”, Str::ascii($name)). Str::ascii transliterates
// -- "ç" becomes "c" -- from a table this module does not carry, so a character
// outside ASCII is dropped rather than approximated; the real name still
// travels, in the filename* parameter. The two characters Symfony refuses in a
// fallback, "/" and "\", are dropped too, along with the control characters.
func responseFactoryFallbackName(name string) string {
	var out strings.Builder
	for _, r := range name {
		if r > unicode.MaxASCII || r == '%' || r == '/' || r == '\\' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		out.WriteRune(r)
	}
	if out.Len() == 0 {
		return "file"
	}
	return out.String()
}

// responseFactoryMakeDisposition answers to Symfony's
// ResponseHeaderBag::makeDisposition, which both streamDownload and download
// build their Content-Disposition with.
func responseFactoryMakeDisposition(disposition, name, fallback string) string {
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(fallback)
	out := disposition + `; filename="` + quoted + `"`
	if name != fallback {
		out += "; filename*=utf-8''" + responseFactoryEncodeRFC5987(name)
	}
	return out
}

// responseFactoryEncodeRFC5987 percent-encodes everything outside the attr-char
// set of RFC 5987, which is what the filename* parameter takes.
func responseFactoryEncodeRFC5987(value string) string {
	const hex = "0123456789ABCDEF"
	const unreserved = "!#$&+-.^_`|~"

	var out strings.Builder
	for _, b := range []byte(value) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9',
			strings.IndexByte(unreserved, b) >= 0:
			out.WriteByte(b)
		default:
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&0x0f])
		}
	}
	return out.String()
}
