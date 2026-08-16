package oauth

import (
	"fmt"
	"sort"
)

// UserData is whatever the provider said about the person who just signed in.
//
// It is a map with methods. [UserData.All] is the whole of it, for a caller
// that wants to range over the fields rather than ask for them by name.
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

// NewUserData wraps the provider's parameters.
//
// The map is kept, not copied: it comes straight off a decoded JSON body that
// nothing else holds.
func NewUserData(params map[string]any) UserData {
	if params == nil {
		params = map[string]any{}
	}
	return UserData{params: params}
}

// All is every parameter the provider sent.
func (u UserData) All() map[string]any {
	if u.params == nil {
		return map[string]any{}
	}
	return u.params
}

// Keys is the parameter names, sorted, so that a log line or a test listing
// them reads the same twice.
func (u UserData) Keys() []string {
	out := make([]string, 0, len(u.params))
	for k := range u.params {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the provider sent this key.
func (u UserData) Has(key string) bool {
	_, ok := u.params[key]
	return ok
}

// Get is the value, or the fallback when the provider did not send that key.
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
// A JSON number arrives as a float64, and an id printed with %v would come out
// as 1.234568e+06, so the conversion happens once, in the place that knows it
// is a conversion.
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
