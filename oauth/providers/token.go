package providers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// AccessToken answers Illuminate\Socialite\OAuthTwo\AccessToken: what the
// provider answered when the code was exchanged.
//
// In PHP it extends ParameterBag and carries whatever came back --
// access_token, refresh_token, expires_in, scope, token_type. It is a map here
// for the same reason, and a map of strings because the two encodings a
// provider replies in disagree about types: an old Facebook returns
// "expires=100" in a form-encoded body, and Google returns {"expires_in":100}
// in JSON. Both arrive here as "100", so a caller reads one thing.
type AccessToken map[string]string

// GetValue answers AccessToken::getValue(): the access token itself.
//
// fallback is Illuminate's optional $default.
func (t AccessToken) GetValue(fallback ...string) string {
	return t.Get("access_token", fallback...)
}

// Get answers ParameterBag::get() for any other parameter the provider sent --
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

// Has answers ParameterBag::has().
func (t AccessToken) Has(key string) bool {
	_, ok := t[key]
	return ok
}

// parseAccessResponse answers Provider::parseAccessResponse() and the
// two overrides of it.
//
// Illuminate needs three versions of this method -- parse_str for the base,
// json_decode for Google and for Stripe -- because each subclass knows what its
// provider replies with. One version reads both: a body that opens with a brace
// is JSON and anything else is form-encoded, and no provider has to be
// configured for something that is written in the reply.
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
