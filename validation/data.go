package validation

import (
	"context"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// Data answers to the $data array Illuminate\Validation\Validator is built
// over: the submitted request, one entry per input name.
//
// PHP's array is a map and a list at once and Go's is neither, so a value here
// is one of: nil -- the null a browser cannot send but a JSON body can --, a
// string, a []any (Laravel's list: what a multi-select or a repeated input
// sends), a Data (Laravel's nested array), or a File. A number or a bool put in
// by hand is read as its PHP equivalent by every rule that asks.
//
// This is the shape `array`, `list`, `distinct`, `contains`, `nullable` and the
// file rules need, and the reason the package no longer stops at url.Values:
// url.Values cannot hold a null, a nested value or an upload, and six of
// Laravel's rules are about exactly those.
type Data map[string]any

// DataFrom builds the Data of a submitted HTML form.
//
// A name sent once is a string and a name sent more than once is a list, which
// is what PHP itself does with `tags[]`: $_POST['tags'] is an array there and a
// string here would make `array` unable to pass on input that really is one. A
// name present with no value at all is the empty list, which `required` refuses
// for the reason PHP's count($value) < 1 does.
func DataFrom(values url.Values) Data {
	d := make(Data, len(values))
	for name, sent := range values {
		switch len(sent) {
		case 1:
			d[name] = sent[0]
		default:
			list := make([]any, len(sent))
			for i, v := range sent {
				list[i] = v
			}
			d[name] = list
		}
	}
	return d
}

// Values renders the Data back as a submitted form, for a caller that hands the
// whole thing on. A value that is not text -- a nested Data, an upload -- has no
// spelling in url.Values and is left out.
func (d Data) Values() url.Values {
	out := make(url.Values, len(d))
	for name, value := range d {
		switch v := value.(type) {
		case nil:
			out[name] = []string{""}
		case string:
			out[name] = []string{v}
		case []any:
			list := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok {
					list = append(list, s)
					continue
				}
				if _, nested := item.(Data); nested {
					continue
				}
				list = append(list, stringOf(item))
			}
			out[name] = list
		case Data:
			continue
		case File:
			continue
		default:
			out[name] = []string{stringOf(v)}
		}
	}
	return out
}

// Has answers to Arr::has for one key, dot notation included.
func (d Data) Has(key string) bool {
	_, ok := lookup(d, key)
	return ok
}

// HasAny answers to Arr::hasAny: at least one of the keys exists. It is what
// required_with, missing_with and present_with ask.
func (d Data) HasAny(keys []string) bool {
	for _, key := range keys {
		if d.Has(key) {
			return true
		}
	}
	return false
}

// HasAll answers to Arr::has given a list of keys: every one exists. It is what
// missing_with_all and present_with_all ask.
func (d Data) HasAll(keys []string) bool {
	for _, key := range keys {
		if !d.Has(key) {
			return false
		}
	}
	return len(keys) > 0
}

// Get answers to Arr::get: the value at the key, nil when there is none.
func (d Data) Get(key string) any {
	v, _ := lookup(d, key)
	return v
}

// Forget answers to Arr::forget: drop the key. Only a top-level key is dropped,
// which is every key an exclude rule names.
func (d Data) Forget(key string) { delete(d, key) }

// Clone is a shallow copy, so that a Validator cannot write through to the
// request's own map.
func (d Data) Clone() Data {
	out := make(Data, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

// lookup walks a dotted path. The literal key is tried first, as Arr::has does,
// so an input genuinely named "a.b" is found before the path a -> b.
func lookup(d Data, key string) (any, bool) {
	if v, ok := d[key]; ok {
		return v, true
	}
	if !strings.Contains(key, ".") {
		return nil, false
	}
	var current any = d
	for _, segment := range strings.Split(key, ".") {
		switch node := current.(type) {
		case Data:
			v, ok := node[segment]
			if !ok {
				return nil, false
			}
			current = v
		case map[string]any:
			v, ok := node[segment]
			if !ok {
				return nil, false
			}
			current = v
		case []any:
			i, err := strconv.Atoi(segment)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			current = node[i]
		default:
			return nil, false
		}
	}
	return current, true
}

// ---------------------------------------------------------------------------
// The value predicates. Each answers to one PHP function, named in its comment,
// because a rule body reads as the PHP does only when these do.
// ---------------------------------------------------------------------------

// asString answers to is_string.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// stringOf is PHP's (string) cast for the values a request carries. A float is
// rendered without a trailing zero, as PHP renders it.
func stringOf(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case string:
		return n
	case bool:
		if n {
			return "1"
		}
		return ""
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32)
	default:
		return ""
	}
}

// asList answers to is_array. A Data is an array in PHP too, and countOf reads
// it, but the rules that walk members want a list and get one only from []any.
func asList(v any) ([]any, bool) {
	list, ok := v.([]any)
	return list, ok
}

// isArray answers to is_array over both shapes PHP's array covers.
func isArray(v any) bool {
	switch v.(type) {
	case []any, Data, map[string]any:
		return true
	}
	return false
}

// countOf answers to count() for a countable value.
func countOf(v any) (int, bool) {
	switch n := v.(type) {
	case []any:
		return len(n), true
	case Data:
		return len(n), true
	case map[string]any:
		return len(n), true
	}
	return 0, false
}

// numberOf answers to is_numeric plus the value it read.
func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case string:
		return number(strings.TrimSpace(n))
	}
	return 0, false
}

// asFile answers to `$value instanceof File`.
func asFile(v any) (File, bool) {
	f, ok := v.(File)
	return f, ok
}

// sameValue answers to PHP's === for the values a request carries. `same` and
// `different` compare with it, and a list compares member by member because
// PHP's === on two arrays does.
func sameValue(a, b any) bool {
	if la, ok := asList(a); ok {
		lb, ok := asList(b)
		if !ok || len(la) != len(lb) {
			return false
		}
		for i := range la {
			if !sameValue(la[i], lb[i]) {
				return false
			}
		}
		return true
	}
	if _, ok := asList(b); ok {
		return false
	}
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

// looseContains answers to in_array with $strict = false over a list of
// parameters, which is how every dependent rule compares the other field.
func looseContains(list []string, v any) bool {
	if v == nil {
		return slices.Contains(list, "null") || slices.Contains(list, "NULL")
	}
	if b, ok := v.(bool); ok {
		want := "false"
		if b {
			want = "true"
		}
		return slices.Contains(list, want) || (b && slices.Contains(list, "1")) ||
			(!b && slices.Contains(list, "0"))
	}
	return slices.Contains(list, stringOf(v))
}

// ---------------------------------------------------------------------------
// What the rules that leave the process need.
//
// The Grant the database rules carry is auth.Grant, the one the whole framework
// carries. This file used to declare a one-method `Grant` interface of its own,
// on the grounds that the package holding the real one was being written by
// other hands at the time. It has landed, and a second type named Grant is a
// second answer to the question RULE 17 asks -- so it is gone.
// ---------------------------------------------------------------------------

// PresenceVerifier answers to
// Illuminate\Validation\PresenceVerifierInterface, with the two arguments RULE
// 17 adds: the context the query runs under and the Grant that authorizes it.
type PresenceVerifier interface {
	// GetCount answers to PresenceVerifierInterface::getCount: how many rows of
	// the collection hold the value in the column, excluding the row whose
	// idColumn is excludeID when one is given, and matching every extra
	// condition.
	GetCount(ctx context.Context, g auth.Grant, collection, column string, value any, excludeID any, idColumn string, extra map[string]string) (int, error)

	// GetMultiCount answers to PresenceVerifierInterface::getMultiCount: how
	// many rows hold any of the values.
	GetMultiCount(ctx context.Context, g auth.Grant, collection, column string, values []any, extra map[string]string) (int, error)
}

// CurrentPasswordChecker is what `current_password` needs.
//
// Laravel resolves the guard and the hasher out of the container, which ADR
// 0002 refuses; the same question asked without one is this.
type CurrentPasswordChecker interface {
	// CheckCurrentPassword reports whether the plain password is the one the
	// subject the Grant was issued to already has. It answers false for a
	// subject that is not authenticated, which is Laravel's $guard->guest().
	CheckCurrentPassword(ctx context.Context, g auth.Grant, guard, password string) bool
}

// Resolver is what `active_url` asks whether a host has an address record.
// *net.Resolver implements it, and net.DefaultResolver is the default.
type Resolver interface {
	// LookupIPAddr answers to dns_get_record($host, DNS_A | DNS_AAAA).
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// File answers to Symfony's File as the validation rules read it: the shape
// `file`, `mimes`, `mimetypes`, `extensions`, `dimensions` and the size rules
// ask about an upload.
//
// It is an interface rather than a struct because the upload itself belongs to
// the HTTP layer. A type there satisfies this by having the methods, with no
// import either way.
type File interface {
	// GetPath answers to SplFileInfo::getPath. An empty path is a file that was
	// never written, which `mimes` and `mimetypes` refuse.
	GetPath() string
	// GetRealPath answers to SplFileInfo::getRealPath: where the bytes are.
	GetRealPath() string
	// GetSize answers to SplFileInfo::getSize, in bytes. The size rules divide
	// it by 1024, because Laravel's `max:100` on a file means kilobytes.
	GetSize() int64
	// GetMimeType answers to File::getMimeType, guessed from the content.
	GetMimeType() string
	// GetExtension answers to SplFileInfo::getExtension.
	GetExtension() string
	// GuessExtension answers to File::guessExtension: the extension implied by
	// the content, which is what `mimes` compares and why a .png renamed to
	// .jpg does not pass.
	GuessExtension() string
}

// UploadedFile answers to Illuminate\Http\UploadedFile: a File that also knows
// what the client called it and whether the upload finished.
type UploadedFile interface {
	File
	// GetClientOriginalExtension answers to
	// UploadedFile::getClientOriginalExtension, which `extensions` compares --
	// the name the browser sent, not the content.
	GetClientOriginalExtension() string
	// IsValid answers to UploadedFile::isValid.
	IsValid() bool
}

// Dimensioner answers to the dimensions() method Illuminate\Http\UploadedFile
// carries. A File that implements it is measured through it; one that does not
// has its bytes decoded, which is what @getimagesize does there.
type Dimensioner interface {
	// Dimensions returns the pixel size of an image, and false when the bytes
	// are not one.
	Dimensions() (width, height int, ok bool)
}
