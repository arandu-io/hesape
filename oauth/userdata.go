package oauth

import (
	"fmt"
	"sort"
)

// UserData answers Illuminate\Socialite\UserData: whatever the provider said
// about the person who just signed in.
//
// In PHP it extends Symfony's ParameterBag, which is a map with methods. This
// is a map with methods, and the methods are the ones that survive the
// translation -- offsetGet, getIterator and count are language interfaces Go
// does not have, and All is what stands in for all three.
//
// The contents are the provider's, not this package's. GitHub calls the handle
// "login" and Google calls it "name"; neither is normalised here, because a
// mapping that guessed would be wrong for the fifth provider somebody adds. Ask
// for what the provider documents:
//
//	user, err := provider.User(r)
//	id := user.String("id")
type UserData struct {
	params map[string]any
}

// NewUserData answers UserData::__construct().
//
// The map is kept, not copied: it comes straight off a decoded JSON body that
// nothing else holds.
func NewUserData(params map[string]any) UserData {
	if params == nil {
		params = map[string]any{}
	}
	return UserData{params: params}
}

// All answers ParameterBag::all(): every parameter the provider sent.
func (u UserData) All() map[string]any {
	if u.params == nil {
		return map[string]any{}
	}
	return u.params
}

// Keys answers ParameterBag::keys(), sorted, so that a log line or a test
// listing them reads the same twice.
func (u UserData) Keys() []string {
	out := make([]string, 0, len(u.params))
	for k := range u.params {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Has answers ParameterBag::has().
func (u UserData) Has(key string) bool {
	_, ok := u.params[key]
	return ok
}

// Get answers ParameterBag::get(): the value, or the fallback when the provider
// did not send that key.
func (u UserData) Get(key string, fallback ...any) any {
	if v, ok := u.params[key]; ok {
		return v
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return nil
}

// String is Get for the common case, where the answer goes into a column.
//
// PHP has no such method because PHP has no such problem: everything there is
// already a string when it is printed. A JSON number arrives here as a float64
// and an id printed with %v would come out as 1.234568e+06, so the conversion
// happens once, in the place that knows it is a conversion.
func (u UserData) String(key string, fallback ...string) string {
	v, ok := u.params[key]
	if !ok || v == nil {
		if len(fallback) > 0 {
			return fallback[0]
		}
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// Whole numbers are ids, and an id is not written with an exponent.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
