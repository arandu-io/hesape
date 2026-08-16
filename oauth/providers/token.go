package providers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// AccessToken is what the provider answered when the code was exchanged.
//
// It carries whatever came back -- access_token, refresh_token, expires_in,
// scope, token_type -- as a map of strings, because the two encodings a
// provider replies in disagree about types: a form-encoded body returns
// "expires=100" and JSON returns {"expires_in":100}. Both arrive here as "100",
// so a caller reads one thing.
type AccessToken map[string]string

// GetValue is the access token itself. fallback is used when the provider sent
// none.
func (t AccessToken) GetValue(fallback ...string) string {
	return t.Get("access_token", fallback...)
}

// Get reads any other parameter the provider sent --
// refresh_token, expires_in, scope.
func (t AccessToken) Get(key string, fallback ...string) string {
	if v, ok := t[key]; ok {
		return v
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

// Has reports whether the provider sent this parameter.
func (t AccessToken) Has(key string) bool {
	_, ok := t[key]
	return ok
}

// parseAccessResponse reads the body of a token exchange.
//
// One implementation reads both encodings: a body that opens with a brace is
// JSON and anything else is form-encoded, so no provider has to be configured
// for something that is already written in the reply.
func parseAccessResponse(body []byte) (AccessToken, error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var raw map[string]any
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return nil, fmt.Errorf("oauth: the access token response is not valid JSON: %w", err)
		}
		out := make(AccessToken, len(raw))
		for k, v := range raw {
			out[k] = scalar(v)
		}
		return out, nil
	}

	values, err := url.ParseQuery(trimmed)
	if err != nil {
		return nil, fmt.Errorf("oauth: the access token response could not be read: %w", err)
	}
	out := make(AccessToken, len(values))
	for k := range values {
		out[k] = values.Get(k)
	}
	return out, nil
}

// scalar renders a JSON value as the string a form-encoded body would have
// carried, so that expires_in is "3600" and not "3600.000000" or "3.6e+03".
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
