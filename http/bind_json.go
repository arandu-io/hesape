package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/validation"
)

// DefaultJSONLimit is the size, in bytes, past which BindJSON refuses a body
// when no JSONLimit says otherwise.
const DefaultJSONLimit int64 = 1 << 20

// JSONBodyKey is the key of the validation.Errors BindJSON returns for a body
// that is not the document dst describes as a whole -- not JSON, empty,
// carrying a field dst does not declare, or followed by anything after the
// document -- as distinct from a field whose value is the wrong type, which is
// keyed by its own path.
const JSONBodyKey = "body"

// JSONOption changes how BindJSON reads a body.
type JSONOption func(*jsonBind)

type jsonBind struct {
	limit int64
}

// JSONLimit sets the largest body BindJSON reads, in bytes. A body over it
// binds nothing and is answered with a *exceptions.PostTooLargeException. It
// takes the limit of an endpoint that knows its documents are small, or one
// that takes a batch; without it the limit is DefaultJSONLimit.
func JSONLimit(bytes int64) JSONOption {
	return func(b *jsonBind) { b.limit = bytes }
}

// BindJSON reads the JSON body of the request into the value dst points to, as
// a typed document: nested objects, arrays and every kind a field of
// encoding/json takes, named by the `json:"name"` tags of dst.
//
//	type StoreEvents struct {
//	    Events []struct {
//	        ID   string `json:"id"`
//	        Cost *struct {
//	            Amount   string `json:"amount"`
//	            Currency string `json:"currency"`
//	        } `json:"cost"`
//	    } `json:"events"`
//	}
//
//	var in StoreEvents
//	if err := ctx.BindJSON(&in, http.JSONLimit(256<<10)); err != nil {
//	    return err
//	}
//
// Bind reads the top-level scalars of a form or a JSON object into tagged
// fields; BindJSON is for the body that is a document. Like Bind, it is the
// struct that lists what may arrive, and it is strict about it:
//
//   - A field the document carries and dst does not declare is refused, not
//     dropped, so a misspelled option or a field the endpoint never took does
//     not pass unnoticed.
//   - Anything after the document, other than whitespace, is refused.
//   - An empty body is refused: the endpoint that calls BindJSON takes a
//     document.
//   - A body over the limit (JSONLimit, or DefaultJSONLimit) binds nothing and
//     is the 413 *exceptions.PostTooLargeException, as it is for Bind; so is a
//     body the server's own limit cut off.
//
// It does not look at the Content-Type: the endpoint that wants to refuse
// another media type says so itself, with the status it owes.
//
// # What it returns
//
// A body that is not the document is a validation.Errors, like a form Bind
// could not convert, so the handler returns it and the adapter answers 422:
//
//   - keyed JSONBodyKey ("body"), for the body as a whole: not JSON, empty,
//     an unknown field, trailing data;
//   - keyed by the path of the field, written with the names of the json tags
//     and a dot, without the position in a list ("events.cost.amount"), for a
//     value of the wrong type or out of range, with the short sentence Bind says for the same failure, and in
//     the application's language when it wired a translator.
//
// Only the first wrong-typed field is reported: encoding/json keeps the first
// and goes on.
//
// A dst that is not a non-nil pointer is a mistake in the program, returned as
// an ordinary error.
//
// # Rules
//
// If dst has a Rules method (validation.RequestRules) the rules run first, over
// the document as a generic object, exactly as they do in Bind: nested fields
// are named with dots ("events.*.id"), a failure is returned as
// validation.Errors with nothing bound, and Context.Validated holds the fields
// that passed. A dst with rules needs a body that is a JSON object.
//
// The body is read once and put back, so a handler that read it first -- to
// hash it for an idempotency key -- or reads it afterwards still sees all of it.
func (c *Context) BindJSON(dst any, options ...JSONOption) error {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("http: BindJSON needs a non-nil pointer, and was given %T", dst)
	}
	settings := jsonBind{limit: DefaultJSONLimit}
	for _, option := range options {
		option(&settings)
	}

	body, err := c.readJSONBody(settings.limit)
	if err != nil {
		return err
	}

	if declaring, ok := dst.(validation.RequestRules); ok {
		var document map[string]any
		if err := json.Unmarshal(body, &document); err != nil || document == nil {
			return validation.Errors{JSONBodyKey: {"must be a JSON object"}}
		}
		data := make(validation.Data, len(document))
		for key, value := range document {
			data[key] = value
		}
		if err := c.runRulesOver(data, declaring); err != nil {
			return err
		}
	}

	errs, err := decodeJSON(body, dst)
	if err != nil {
		return err
	}
	if errs.Any() {
		return c.localizeConversion(errs, requestAttributes(dst))
	}
	return nil
}

// DecodeJSON is BindJSON for a document that is not the body of a request: a
// JSON value held in a field of another, or read from somewhere that is not the
// request. It decodes body into the value dst points to with the same
// strictness -- an unknown field, anything after the document and an empty body
// are refused -- and reports a document that is not the one dst describes as
// the same validation.Errors, keyed JSONBodyKey or by the path of the field.
//
// It has no request, so it applies no size limit, runs no Rules and says its
// sentences in English; the caller that holds a request binds through
// Context.BindJSON.
func DecodeJSON(body []byte, dst any) error {
	errs, err := decodeJSON(body, dst)
	if err != nil {
		return err
	}
	if errs.Any() {
		return errs
	}
	return nil
}

// decodeJSON is the strict decode both BindJSON and DecodeJSON stand on. It
// returns the faults of the document as errs, and an error only for a dst the
// decoder cannot write into, which is the program's mistake.
func decodeJSON(body []byte, dst any) (validation.Errors, error) {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return nil, fmt.Errorf("http: decoding JSON needs a non-nil pointer, and was given %T", dst)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return validation.Errors{JSONBodyKey: {"must be a JSON document"}}, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	errs := validation.Errors{}
	if err := decoder.Decode(dst); err != nil {
		if !jsonFailure(err, errs) {
			return nil, err
		}
	}
	if !errs.Any() {
		if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
			errs.Add(JSONBodyKey, "must be a single JSON document")
		}
	}
	return errs, nil
}

// readJSONBody reads the body up to limit bytes and puts it back for the next
// reader.
func (c *Context) readJSONBody(limit int64) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil {
		if tooLarge := bodyTooLarge(err); tooLarge != nil {
			return nil, tooLarge
		}
		return nil, fmt.Errorf("http: reading the JSON body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, bodyTooLarge(&stdhttp.MaxBytesError{Limit: limit})
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// jsonFailure files err, which the decoder returned, as a message on the body
// or on the field it names. It reports false for an error that is not the
// request's: the decoder was handed a type it cannot decode into.
func jsonFailure(err error, errs validation.Errors) bool {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	var invalid *json.InvalidUnmarshalError
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &syntax):
		errs.Add(JSONBodyKey, "must be valid JSON")
	case errors.As(err, &typed):
		field := jsonFieldPath(typed.Field)
		errs.Add(field, jsonTypeMessage(typed))
	case errors.As(err, &invalid):
		return false
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		errs.Add(JSONBodyKey, "has an unknown field "+strings.TrimPrefix(err.Error(), "json: unknown field "))
	default:
		errs.Add(JSONBodyKey, "must be a JSON document of the shape this request takes")
	}
	return true
}

// jsonFieldPath is the path encoding/json reports for a value, without the
// positions in a list. The toolchain decides whether the path carries them
// ("events.0.qty" or "events.qty"), and a key that depends on the Go that built
// the program is not one a client can be told about.
func jsonFieldPath(path string) string {
	var kept []string
	for _, segment := range strings.Split(path, ".") {
		if segment == "" || strings.Trim(segment, "0123456789") == "" {
			continue
		}
		kept = append(kept, segment)
	}
	if len(kept) == 0 {
		return JSONBodyKey
	}
	return strings.Join(kept, ".")
}

// jsonTypeMessage is the short sentence for a value that did not fit its field,
// the one Bind says for the same kind of field where there is one.
func jsonTypeMessage(e *json.UnmarshalTypeError) string {
	switch e.Type.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if number, ok := strings.CutPrefix(e.Value, "number "); ok {
			if _, err := strconv.ParseFloat(number, 64); err == nil && !strings.ContainsAny(number, ".eE") {
				return "is out of range"
			}
		}
		return "must be a whole number"
	case reflect.Float32, reflect.Float64:
		return "must be a number"
	case reflect.Bool:
		return "must be true or false"
	case reflect.String:
		return "must be text"
	case reflect.Slice, reflect.Array:
		return "must be a list"
	case reflect.Struct, reflect.Map:
		return "must be an object"
	}
	return "is not the type this field takes"
}
