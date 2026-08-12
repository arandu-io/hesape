package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	stdhttp "net/http"

	"github.com/arandu-io/hesape/support"
)

// The encoding flags answer to the PHP JSON_* constants JsonResponse takes as
// its $options argument and HasEncodingOption tests against. The values are
// PHP's, because the whole point of HasEncodingOption is that a caller reads a
// flag it set, and a renumbered set would answer a different question.
//
// encoding/json has no flag word, so only the flags that change what Go can
// emit are honoured: JSONPrettyPrint indents, JSONUnescapedSlashes and
// JSONUnescapedUnicode turn off Go's HTML escaping, and
// JSONPartialOutputOnError is read by SetData when deciding whether an encoding
// failure is fatal. The rest are accepted, stored and reported by
// HasEncodingOption, and change nothing -- as they would in PHP for a payload
// that does not contain what they are about.
const (
	// JSONHexTag answers to PHP's JSON_HEX_TAG.
	JSONHexTag = 1
	// JSONHexAmp answers to PHP's JSON_HEX_AMP.
	JSONHexAmp = 2
	// JSONHexApos answers to PHP's JSON_HEX_APOS.
	JSONHexApos = 4
	// JSONHexQuot answers to PHP's JSON_HEX_QUOT.
	JSONHexQuot = 8
	// JSONForceObject answers to PHP's JSON_FORCE_OBJECT.
	JSONForceObject = 16
	// JSONNumericCheck answers to PHP's JSON_NUMERIC_CHECK.
	JSONNumericCheck = 32
	// JSONUnescapedSlashes answers to PHP's JSON_UNESCAPED_SLASHES.
	JSONUnescapedSlashes = 64
	// JSONPrettyPrint answers to PHP's JSON_PRETTY_PRINT.
	JSONPrettyPrint = 128
	// JSONUnescapedUnicode answers to PHP's JSON_UNESCAPED_UNICODE.
	JSONUnescapedUnicode = 256
	// JSONPartialOutputOnError answers to PHP's JSON_PARTIAL_OUTPUT_ON_ERROR.
	JSONPartialOutputOnError = 512
	// JSONPreserveZeroFraction answers to PHP's JSON_PRESERVE_ZERO_FRACTION.
	JSONPreserveZeroFraction = 1024
	// JSONThrowOnError answers to PHP's JSON_THROW_ON_ERROR.
	JSONThrowOnError = 4194304
)

// JsonResponse mirrors Illuminate\Http\JsonResponse: a Response whose body is
// the JSON encoding of a value, with the encoding flags kept so that
// SetEncodingOptions can re-encode what is already there.
//
// It embeds Response, which is how the PHP `extends BaseJsonResponse` plus
// `use ResponseTrait` arrives in Go: Status, Header, WithCookie, ThrowResponse
// and the rest are the same methods on the same fields.
type JsonResponse struct {
	Response

	// data is the encoded payload. Symfony keeps it separate from the content
	// because the JSONP callback wraps it.
	data string
	// encodingOptions is the $options word.
	encodingOptions int
}

// NewJsonResponse answers to JsonResponse::__construct.
//
// The variadic arguments are the PHP's defaults, in order: status (200),
// headers (none), options (0) and json (false -- whether data is already an
// encoded JSON string).
//
// The PHP throws InvalidArgumentException when encoding fails, so this returns
// (*JsonResponse, error).
func NewJsonResponse(data any, args ...any) (*JsonResponse, error) {
	status := stdhttp.StatusOK
	var headers stdhttp.Header
	options := 0
	alreadyJSON := false

	if len(args) > 0 {
		if value, ok := args[0].(int); ok {
			status = value
		}
	}
	if len(args) > 1 {
		switch value := args[1].(type) {
		case stdhttp.Header:
			headers = value
		case map[string]string:
			headers = stdhttp.Header{}
			for key, item := range value {
				headers.Set(key, item)
			}
		}
	}
	if len(args) > 2 {
		if value, ok := args[2].(int); ok {
			options = value
		}
	}
	if len(args) > 3 {
		if value, ok := args[3].(bool); ok {
			alreadyJSON = value
		}
	}

	r := &JsonResponse{
		Response: Response{
			status:          status,
			headers:         stdhttp.Header{},
			protocolVersion: "1.0",
		},
		encodingOptions: options,
	}
	r.Response.headers.Set("Content-Type", "application/json")
	r.WithHeaders(headers)

	if alreadyJSON {
		r.setJSONString(stringify(data))
		return r, nil
	}
	if _, err := r.SetData(data); err != nil {
		return nil, err
	}
	return r, nil
}

// FromJsonString answers to JsonResponse::fromJsonString: build a response
// around a string that is already encoded JSON.
//
// It is a package function and not a method because the PHP is static. The
// variadic arguments are the PHP's status (200) and headers (none).
func FromJsonString(data string, args ...any) (*JsonResponse, error) {
	rest := make([]any, 0, 4)
	if len(args) > 0 {
		rest = append(rest, args[0])
	} else {
		rest = append(rest, stdhttp.StatusOK)
	}
	if len(args) > 1 {
		rest = append(rest, args[1])
	} else {
		rest = append(rest, stdhttp.Header(nil))
	}
	rest = append(rest, 0, true)
	return NewJsonResponse(data, rest...)
}

// GetData answers to JsonResponse::getData: the payload decoded back out of the
// encoded body.
//
// The PHP takes $assoc and $depth, which choose between an object and an
// associative array and cap the nesting; encoding/json has neither -- it
// decodes into map[string]any either way and has no depth limit -- so the two
// arguments are accepted and ignored, and this returns (any, error) where the
// PHP returns null on a decode failure.
func (r *JsonResponse) GetData(args ...any) (any, error) {
	var decoded any
	if r.data == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(r.data), &decoded); err != nil {
		return nil, fmt.Errorf("http: the response payload is not valid JSON: %w", err)
	}
	return decoded, nil
}

// SetData answers to JsonResponse::setData: encode the value and make it the
// body. It keeps the value on $original, which is what GetOriginalContent
// returns.
//
// The PHP throws InvalidArgumentException when encoding fails, unless
// JSONPartialOutputOnError is set; this returns the error, and honours the same
// flag by falling back to an empty object.
func (r *JsonResponse) SetData(data any) (*JsonResponse, error) {
	r.original = data

	encoded, err := r.encode(data)
	if err != nil {
		if r.HasEncodingOption(JSONPartialOutputOnError) {
			r.setJSONString("{}")
			return r, nil
		}
		return nil, fmt.Errorf("http: the response payload could not be encoded as JSON: %w", err)
	}
	r.setJSONString(encoded)
	return r, nil
}

// encode applies the encoding flags encoding/json can express.
func (r *JsonResponse) encode(data any) (string, error) {
	switch typed := data.(type) {
	case support.Jsonable:
		return typed.ToJson()
	case Arrayable:
		data = typed.ToArray()
	case JsonSerializable:
		data = typed.JsonSerialize()
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	// JSON_UNESCAPED_SLASHES and JSON_UNESCAPED_UNICODE both mean "stop
	// rewriting characters that were already valid", which in encoding/json is
	// the one switch SetEscapeHTML.
	if r.encodingOptions&(JSONUnescapedSlashes|JSONUnescapedUnicode) != 0 {
		encoder.SetEscapeHTML(false)
	}
	if r.encodingOptions&JSONPrettyPrint != 0 {
		encoder.SetIndent("", "    ")
	}
	if err := encoder.Encode(data); err != nil {
		return "", err
	}
	// Encode appends a newline that the PHP json_encode does not.
	return string(bytes.TrimRight(buffer.Bytes(), "\n")), nil
}

// setJSONString writes the payload and rebuilds the body, wrapping it in the
// JSONP callback when there is one. It answers to Symfony's update().
func (r *JsonResponse) setJSONString(data string) {
	r.data = data
	if r.callback != "" {
		r.Response.headers.Set("Content-Type", "text/javascript")
		r.content = "/**/" + r.callback + "(" + data + ");"
		return
	}
	r.Response.headers.Set("Content-Type", "application/json")
	r.content = data
}

// SetEncodingOptions answers to JsonResponse::setEncodingOptions: change the
// flags and re-encode what is already there.
func (r *JsonResponse) SetEncodingOptions(options int) (*JsonResponse, error) {
	r.encodingOptions = options
	return r.SetData(r.original)
}

// GetEncodingOptions answers to Symfony's JsonResponse::getEncodingOptions.
func (r *JsonResponse) GetEncodingOptions() int { return r.encodingOptions }

// HasEncodingOption answers to JsonResponse::hasEncodingOption: whether a flag
// is set in the options word.
func (r *JsonResponse) HasEncodingOption(option int) bool {
	return r.encodingOptions&option != 0
}

// WithCallback answers to JsonResponse::withCallback: set the JSONP callback.
func (r *JsonResponse) WithCallback(callback string) *JsonResponse {
	return r.SetCallback(callback)
}

// SetCallback answers to Symfony's JsonResponse::setCallback, which
// withCallback calls.
func (r *JsonResponse) SetCallback(callback string) *JsonResponse {
	r.callback = callback
	r.setJSONString(r.data)
	return r
}

// The next block redeclares the ResponseTrait methods that return $this. Go has
// no covariant return type, so the promoted method from the embedded Response
// would hand back a *Response and end the chain; PHP's $this keeps its class.

// Header is ResponseTrait::header, typed for the chain.
func (r *JsonResponse) Header(key string, values ...any) *JsonResponse {
	r.Response.Header(key, values...)
	return r
}

// WithHeaders is ResponseTrait::withHeaders, typed for the chain.
func (r *JsonResponse) WithHeaders(headers stdhttp.Header) *JsonResponse {
	r.Response.WithHeaders(headers)
	return r
}

// WithoutHeader is ResponseTrait::withoutHeader, typed for the chain.
func (r *JsonResponse) WithoutHeader(keys ...string) *JsonResponse {
	r.Response.WithoutHeader(keys...)
	return r
}

// Cookie is ResponseTrait::cookie, typed for the chain.
func (r *JsonResponse) Cookie(cookie *stdhttp.Cookie) *JsonResponse {
	return r.WithCookie(cookie)
}

// WithCookie is ResponseTrait::withCookie, typed for the chain.
func (r *JsonResponse) WithCookie(cookie *stdhttp.Cookie) *JsonResponse {
	r.Response.WithCookie(cookie)
	return r
}

// WithoutCookie is ResponseTrait::withoutCookie, typed for the chain.
func (r *JsonResponse) WithoutCookie(name string, args ...string) *JsonResponse {
	r.Response.WithoutCookie(name, args...)
	return r
}

// WithException is ResponseTrait::withException, typed for the chain.
func (r *JsonResponse) WithException(err error) *JsonResponse {
	r.Response.WithException(err)
	return r
}

// SetStatusCode is Symfony's Response::setStatusCode, typed for the chain.
func (r *JsonResponse) SetStatusCode(code int) *JsonResponse {
	r.Response.SetStatusCode(code)
	return r
}
