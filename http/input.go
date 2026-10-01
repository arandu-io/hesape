package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"net/url"

	"github.com/arandu-io/hesape/validation"
)

// multipartMaxMemory is how much of a multipart body is held in memory while it
// is parsed; the rest of the files go to temporary files, which are removed
// when the request ends. It is the amount net/http's own FormValue uses.
const multipartMaxMemory = 32 << 20

// formBodyMaxBytes caps a url-encoded body read for a method net/http does not
// read one for, at the size net/http caps its own read at.
const formBodyMaxBytes = 10 << 20

// readsBody reports whether a request with this method carries its input in
// the body. GET and HEAD carry it in the query string; every other method
// carries it in the body, and for those the query string is never input.
func readsBody(method string) bool {
	switch method {
	case "", stdhttp.MethodGet, stdhttp.MethodHead:
		return false
	}
	return true
}

// Input is a value from the request's input, read with dot notation.
//
// The input is one map, and it is the same map All, Only, Validate,
// Context.Input and Context.Bind read, so what a rule validated is exactly
// what a handler reads afterwards. For GET and HEAD it is the query string.
// For every other method it is the body and only the body: the JSON payload
// for a JSON request, otherwise the url-encoded or multipart form's text
// fields. The query string of such a request is never merged in -- a value in
// the action URL of a form must not stand in for a field the body did not
// send -- and stays readable on its own through Query.
//
// A JSON body is read the same way as form input: Input("user.name")
// descends by dot through the decoded payload, so a nested JSON field is
// found the same way a flattened form field is.
//
// With no key, returns the whole map.
func (r *Request) Input(key string, def ...any) any {
	merged := r.inputMap()
	if key == "" {
		return merged
	}
	if len(def) > 0 {
		return dataGet(merged, key, def[0])
	}
	return dataGet(merged, key, nil)
}

// inputMap is the request's input: the input source, and nothing merged into
// it.
func (r *Request) inputMap() map[string]any {
	return r.inputSource()
}

// inputSource returns the query string for GET/HEAD, the JSON payload for a
// JSON request, and the body's form fields for everything else. A multipart
// body is parsed before its fields are read, so its text fields are input
// like a url-encoded body's are.
func (r *Request) inputSource() map[string]any {
	if !readsBody(r.request.Method) {
		return valuesToMap(r.request.URL.Query())
	}
	if r.IsJSON() {
		return r.jsonPayload()
	}
	_ = parseBody(r.request)
	return valuesToMap(r.request.PostForm)
}

// parseBody reads the body's form into r.PostForm, and nothing of the query
// string into it: a url-encoded body for any method, including DELETE, which
// net/http does not read, and the text fields of a multipart body. A multipart
// body that leaves temporary files behind has them removed when the request
// ends, by removeFormAtEnd.
func parseBody(r *stdhttp.Request) error {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "multipart/form-data":
		return parseMultipart(r)
	case "application/x-www-form-urlencoded":
		if r.PostForm == nil && !parsesPostForm(r.Method) && r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, formBodyMaxBytes+1))
			if err != nil {
				return err
			}
			if len(body) > formBodyMaxBytes {
				return errors.New("http: url-encoded body too large")
			}
			values, err := url.ParseQuery(string(body))
			r.PostForm = values
			if r.PostForm == nil {
				r.PostForm = url.Values{}
			}
			if err != nil {
				return err
			}
		}
	}
	return r.ParseForm()
}

// parsesPostForm reports whether net/http's ParseForm reads a url-encoded body
// for the method.
func parsesPostForm(method string) bool {
	return method == stdhttp.MethodPost || method == stdhttp.MethodPut || method == stdhttp.MethodPatch
}

// parseMultipart parses a multipart body once, with the text fields joining
// r.PostForm and the files r.MultipartForm.
func parseMultipart(r *stdhttp.Request) error {
	if r.MultipartForm != nil {
		return nil
	}
	return r.ParseMultipartForm(multipartMaxMemory)
}

// data is the value at the key from the input source, without the query
// string. It is the helper Filled, Boolean and the other scalar readers use,
// so that a query string value does not shadow a body value for them.
func (r *Request) data(key string, def ...any) any {
	source := r.inputSource()
	if key == "" {
		return source
	}
	if len(def) > 0 {
		return dataGet(source, key, def[0])
	}
	return dataGet(source, key, nil)
}

// valuesToMap converts a url.Values to a map[string]any: a single value is a
// string, a repeated key is a list.
func valuesToMap(values url.Values) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		switch len(v) {
		case 0:
			out[k] = ""
		case 1:
			out[k] = v[0]
		default:
			list := make([]any, len(v))
			for i, item := range v {
				list[i] = item
			}
			out[k] = list
		}
	}
	return out
}

// All is the input and the files, merged. With keys, returns only those
// keys.
func (r *Request) All(keys ...string) map[string]any {
	input := r.inputMap()
	files := r.allFilesMap()
	merged := make(map[string]any, len(input)+len(files))
	for k, v := range input {
		merged[k] = v
	}
	for k, v := range files {
		merged[k] = v
	}
	if len(keys) == 0 {
		return merged
	}
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		dataSet(result, key, dataGet(merged, key, nil))
	}
	return result
}

// Only is a subset of the input containing only the given keys.
func (r *Request) Only(keys ...string) map[string]any {
	all := r.All()
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if arrHas(all, key) {
			dataSet(result, key, dataGet(all, key, nil))
		}
	}
	return result
}

// Except is the input without the given keys.
func (r *Request) Except(keys ...string) map[string]any {
	result := r.All()
	arrForget(result, keys)
	return result
}

// Has reports whether every given key exists in the input.
func (r *Request) Has(keys ...string) bool {
	all := r.All()
	for _, key := range keys {
		if !arrHas(all, key) {
			return false
		}
	}
	return true
}

// HasAny reports whether at least one of the keys exists.
func (r *Request) HasAny(keys ...string) bool {
	return arrHasAny(r.All(), keys)
}

// Missing is the negation of Has.
func (r *Request) Missing(keys ...string) bool {
	return !r.Has(keys...)
}

// Filled reports whether every given key has a non-empty value. A boolean
// true and a non-empty list count as filled; an empty string or a string of
// whitespace does not.
func (r *Request) Filled(keys ...string) bool {
	for _, key := range keys {
		if r.isEmptyString(key) {
			return false
		}
	}
	return true
}

// IsNotFilled reports whether every given key is empty.
func (r *Request) IsNotFilled(keys ...string) bool {
	for _, key := range keys {
		if !r.isEmptyString(key) {
			return false
		}
	}
	return true
}

// AnyFilled reports whether at least one key is filled.
func (r *Request) AnyFilled(keys ...string) bool {
	for _, key := range keys {
		if r.Filled(key) {
			return true
		}
	}
	return false
}

// WhenHas calls the callback with the value when the key exists, otherwise
// calls the default. Returns the callback's result, or the request itself
// when the callback returns nil.
func (r *Request) WhenHas(key string, callback func(value any) any, def ...func() any) any {
	if r.Has(key) {
		result := callback(dataGet(r.All(), key, nil))
		if result != nil {
			return result
		}
		return r
	}
	if len(def) > 0 && def[0] != nil {
		return def[0]()
	}
	return r
}

// WhenFilled calls the callback with the value when the key is filled,
// otherwise calls the default.
func (r *Request) WhenFilled(key string, callback func(value any) any, def ...func() any) any {
	if r.Filled(key) {
		result := callback(dataGet(r.All(), key, nil))
		if result != nil {
			return result
		}
		return r
	}
	if len(def) > 0 && def[0] != nil {
		return def[0]()
	}
	return r
}

// WhenMissing calls the callback when the key is missing, otherwise calls
// the default.
func (r *Request) WhenMissing(key string, callback func(value any) any, def ...func() any) any {
	if r.Missing(key) {
		result := callback(dataGet(r.All(), key, nil))
		if result != nil {
			return result
		}
		return r
	}
	if len(def) > 0 && def[0] != nil {
		return def[0]()
	}
	return r
}

// isEmptyString reports whether the value at the key is an empty string (or
// whitespace), excluding booleans and lists.
func (r *Request) isEmptyString(key string) bool {
	value := r.data(key)
	if _, ok := value.(bool); ok {
		return false
	}
	if _, ok := value.([]any); ok {
		return false
	}
	if _, ok := value.(map[string]any); ok {
		return false
	}
	return strings.TrimSpace(stringify(value)) == ""
}

// Boolean is the value as a bool. Returns true for "1", "true", "on", "yes"
// (case-insensitive).
func (r *Request) Boolean(key string, def ...bool) bool {
	d := false
	if len(def) > 0 {
		d = def[0]
	}
	value := r.data(key)
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return truthy(v)
	case nil:
		return d
	}
	return d
}

// truthy reports whether a form value spells true: "1", "true", "on" or "yes",
// in any case. "on" is what a checked checkbox with no value attribute sends.
func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// falsy reports whether a form value spells false: "", "0", "false", "off" or
// "no", in any case.
func falsy(s string) bool {
	switch strings.ToLower(s) {
	case "", "0", "false", "off", "no":
		return true
	}
	return false
}

// Integer is the value as an int64.
func (r *Request) Integer(key string, def ...int64) int64 {
	d := int64(0)
	if len(def) > 0 {
		d = def[0]
	}
	value := r.data(key)
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return d
		}
		return n
	case nil:
		return d
	}
	return d
}

// Float is the value as a float64.
func (r *Request) Float(key string, def ...float64) float64 {
	d := float64(0)
	if len(def) > 0 {
		d = def[0]
	}
	value := r.data(key)
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return d
		}
		return n
	case nil:
		return d
	}
	return d
}

// String is the value as a string.
func (r *Request) String(key string, def ...string) string {
	d := ""
	if len(def) > 0 {
		d = def[0]
	}
	value := r.data(key)
	if value == nil {
		return d
	}
	return stringify(value)
}

// Str is an alias for String.
func (r *Request) Str(key string, def ...string) string {
	return r.String(key, def...)
}

// Date is the value parsed as a time.Time. Without a format, parses as
// RFC3339; with a format, parses against it. Returns the zero time when the
// key is not filled or the value does not parse.
func (r *Request) Date(key string, format ...string) (time.Time, bool) {
	if r.IsNotFilled(key) {
		return time.Time{}, false
	}
	value := r.String(key)
	if value == "" {
		return time.Time{}, false
	}
	if len(format) == 0 {
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	t, err := time.Parse(format[0], value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Enum is the value as an enum, via a tryFrom function the caller supplies.
// Returns nil when the key is not filled or the value does not match any
// enum case.
//
// The caller passes the function that turns a string into the enum value
// and reports whether it matched. A Go enum that implements a TryFrom
// method can hand it directly:
//
//	status, ok := req.Enum("status", StatusTryFrom)
func (r *Request) Enum(key string, tryFrom func(string) (any, bool)) any {
	if r.IsNotFilled(key) {
		return nil
	}
	v, ok := tryFrom(r.String(key))
	if !ok {
		return nil
	}
	return v
}

// Array is the value as a []any. When more than one key is given, returns
// Only for those keys.
func (r *Request) Array(keys ...string) []any {
	if len(keys) > 1 {
		only := r.Only(keys...)
		out := make([]any, 0, len(only))
		for _, v := range only {
			out = append(out, v)
		}
		return out
	}
	key := ""
	if len(keys) == 1 {
		key = keys[0]
	}
	value := r.data(key)
	switch v := value.(type) {
	case []any:
		return v
	case nil:
		return nil
	default:
		return []any{v}
	}
}

// Collect is the value as a []any, which is the shape a
// hesape/collections.Collection is built from. With more than one key,
// returns Only for those keys as a slice.
func (r *Request) Collect(keys ...string) []any {
	if len(keys) > 1 {
		only := r.Only(keys...)
		out := make([]any, 0, len(only))
		for _, v := range only {
			out = append(out, v)
		}
		return out
	}
	key := ""
	if len(keys) == 1 {
		key = keys[0]
	}
	value := r.data(key)
	switch v := value.(type) {
	case []any:
		return v
	case nil:
		return []any{}
	case map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return []any{v}
	}
}

// Fluent is the input as a map that reads with dot notation. It is the
// same input Input reads, and a read on a missing key returns
// nil rather than panicking.
func (r *Request) Fluent(key string, def ...map[string]any) map[string]any {
	if key == "" {
		return r.inputMap()
	}
	_ = def
	return r.Only(key)
}

// Query is a query string parameter, or a default when it is absent. With
// an empty key, returns empty.
func (r *Request) Query(key string, def ...string) string {
	values := r.request.URL.Query()
	if key == "" {
		return ""
	}
	value := values.Get(key)
	if value != "" {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// Post is a post body parameter, or a default when it is absent. With an
// empty key, returns empty.
func (r *Request) Post(key string, def ...string) string {
	_ = parseBody(r.request)
	values := r.request.PostForm
	if key == "" {
		return ""
	}
	value := values.Get(key)
	if value != "" {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// File is the uploaded file(s) at the key, or nil. Returns
// *multipart.FileHeader, which [UploadedFile] wraps into the fuller file
// surface.
func (r *Request) File(key string) any {
	files := r.allFilesMap()
	return dataGet(files, key, nil)
}

// HasFile reports whether a valid file is present at the key.
func (r *Request) HasFile(key string) bool {
	file := r.File(key)
	headers, ok := file.([]*multipart.FileHeader)
	if ok {
		for _, h := range headers {
			if h != nil && h.Size > 0 {
				return true
			}
		}
		return false
	}
	header, ok := file.(*multipart.FileHeader)
	return ok && header != nil && header.Size > 0
}

// AllFiles is every uploaded file, keyed by the form field name.
func (r *Request) AllFiles() map[string]any {
	return r.allFilesMap()
}

// allFilesMap parses the multipart form (if any) and returns the files as a
// map. A single upload is a *multipart.FileHeader; a repeated field is a
// []*multipart.FileHeader.
func (r *Request) allFilesMap() map[string]any {
	if err := parseMultipart(r.request); err != nil {
		return map[string]any{}
	}
	if r.request.MultipartForm == nil || len(r.request.MultipartForm.File) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(r.request.MultipartForm.File))
	for key, headers := range r.request.MultipartForm.File {
		if len(headers) == 1 {
			out[key] = headers[0]
			continue
		}
		list := make([]any, len(headers))
		for i, h := range headers {
			list[i] = h
		}
		out[key] = list
	}
	return out
}

// Validate runs the rules against the request's input and returns the
// validated values, or the error the failures make.
//
// The rules are a compiled *validation.Set, built at boot with
// validation.MustCompile. The options carry the context, the Grant and the
// collaborators the rules that leave the process need.
func (r *Request) Validate(rules *validation.Set, opts ...validation.ValidatorOption) (validation.Input, error) {
	data := r.validationData()
	return validation.Make(data, rules, opts...).Validate()
}

// ValidateWithBag is Validate with the failures named, so that two forms on
// one page do not draw each other's errors.
func (r *Request) ValidateWithBag(bag string, rules *validation.Set, opts ...validation.ValidatorOption) (validation.Input, error) {
	data := r.validationData()
	return validation.Make(data, rules, opts...).ValidateWithBag(bag)
}

// validationData builds the validation.Data from the request's input. Public
// input access keeps returning multipart headers, while validation receives
// UploadedFile values whose security metadata comes from the bytes.
func (r *Request) validationData() validation.Data {
	all := r.All()
	data := make(validation.Data, len(all))
	for k, v := range all {
		data[k] = validationValue(k, v)
	}
	return data
}

func validationValue(field string, value any) any {
	switch files := value.(type) {
	case *multipart.FileHeader:
		return NewUploadedFile(files, field)
	case []*multipart.FileHeader:
		wrapped := make([]any, len(files))
		for i, file := range files {
			wrapped[i] = NewUploadedFile(file, field)
		}
		return wrapped
	case []any:
		wrapped := make([]any, len(files))
		for i, file := range files {
			wrapped[i] = validationValue(field, file)
		}
		return wrapped
	default:
		return value
	}
}

// jsonBodyBytes reads and restores the body, returning the raw bytes. It is
// used by jsonPayload and by tests that need to inspect the body.
func (r *Request) jsonBodyBytes() []byte {
	body, err := io.ReadAll(r.request.Body)
	if err != nil {
		return nil
	}
	r.request.Body = io.NopCloser(strings.NewReader(string(body)))
	return body
}

// jsonDecode decodes a JSON body into a map, returning an empty map for an
// empty or invalid body.
func jsonDecode(body []byte) map[string]any {
	if len(body) == 0 {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return map[string]any{}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}
