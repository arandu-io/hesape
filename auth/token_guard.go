package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// TokenGuard is Illuminate\Auth\TokenGuard: the API token guard.
//
// It reads a token off the request and asks the provider for the user whose
// stored token column matches. There is no session, no cookie and nothing to
// log out of: every request proves itself again.
//
// It is the simplest thing that works and it is not a token system. There is no
// expiry, no scope and no revocation list here -- those belong to whatever
// issues the tokens.
type TokenGuard struct {
	GuardHelpers

	// request is $request.
	request Request

	// inputKey is $inputKey: the query string or input field the token arrives
	// in.
	inputKey string

	// storageKey is $storageKey: the column it is kept in.
	storageKey string

	// hash is $hash: the stored token is a sha256 of the one on the wire.
	hash bool
}

var _ Guard = (*TokenGuard)(nil)

// NewTokenGuard is TokenGuard::__construct.
//
// The PHP defaults inputKey and storageKey to "api_token" and hash to false;
// Go has no default arguments, so all three are passed. An empty inputKey or
// storageKey becomes "api_token", which is the only way to write the PHP's
// default down in a place a caller cannot forget.
func NewTokenGuard(provider UserProvider, request Request, inputKey, storageKey string, hash bool) *TokenGuard {
	if inputKey == "" {
		inputKey = "api_token"
	}
	if storageKey == "" {
		storageKey = "api_token"
	}

	guard := &TokenGuard{
		request:    request,
		inputKey:   inputKey,
		storageKey: storageKey,
		hash:       hash,
	}
	guard.provider = provider
	guard.resolveUser = guard.User

	return guard
}

// User is TokenGuard::user: the user the request's token names, or nil.
//
// It resolves once per request and caches, including the nil: a request with a
// token nobody holds does not go back to the store on every call.
func (g *TokenGuard) User() Authenticatable {
	// If we have already retrieved the user for the current request we can just
	// return it back immediately.
	if g.user != nil {
		return g.user
	}

	var user Authenticatable

	if token := g.GetTokenForRequest(); token != "" {
		value := token
		if g.hash {
			sum := sha256.Sum256([]byte(token))
			value = hex.EncodeToString(sum[:])
		}

		if found, err := g.provider.RetrieveByCredentials(g.context(), map[string]any{g.storageKey: value}); err == nil {
			user = found
		}
	}

	g.user = user

	return g.user
}

// GetTokenForRequest is TokenGuard::getTokenForRequest: the token, from the
// four places it may arrive in, in the PHP's order.
//
// Query string first, then the input, then the Authorization bearer, then the
// HTTP Basic password. The last one is how a token is handed to a tool that
// only knows how to do Basic auth.
func (g *TokenGuard) GetTokenForRequest() string {
	if g.request == nil {
		return ""
	}

	token := g.request.Query(g.inputKey)

	if token == "" {
		// Input answers with the PHP's mixed. A token that is not a string is
		// not a token.
		if value, ok := g.request.Input(g.inputKey).(string); ok {
			token = value
		}
	}

	if token == "" {
		token = g.request.BearerToken()
	}

	if token == "" {
		token = g.request.GetPassword()
	}

	return token
}

// Validate is TokenGuard::validate: is this token good, without resolving the
// guard's user.
func (g *TokenGuard) Validate(ctx context.Context, credentials map[string]any) bool {
	token, ok := credentials[g.inputKey]
	if !ok || isEmptyCredential(token) {
		return false
	}

	user, err := g.provider.RetrieveByCredentials(ctx, map[string]any{g.storageKey: token})

	return err == nil && user != nil
}

// SetRequest is TokenGuard::setRequest.
func (g *TokenGuard) SetRequest(request Request) *TokenGuard {
	g.request = request

	return g
}

// context is the context the lookup runs on. See [Request] for why the guard
// reads it off the request.
func (g *TokenGuard) context() context.Context {
	if g.request != nil {
		if ctx := g.request.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

// isEmptyCredential is the empty() the PHP guards the credential with: null and
// the empty string are nothing, and so is a value that is there but blank.
func isEmptyCredential(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}
