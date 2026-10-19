package exception

import (
	"context"
	"net/http"
)

// Sentences chooses what a person reads when a request is refused with a
// status: the 403 of a guard or a policy, the 404 of a missing record, the 419
// of an expired form, the detail of a problem document.
//
// It answers the sentence for this request and this status, or the empty
// string to keep the one the framework wrote. The request is passed so an
// application can say different things in different places -- "this area is
// for the team" under its console, "you do not have access" elsewhere -- and
// read the language a person asked for.
//
// The framework's sentences are English and written for any application; an
// application in another language, or with its own voice, replaces them here
// rather than by catching every refusal on its way out.
type Sentences func(r *http.Request, status int) string

// sentencesKey is the context key of the application's Sentences, of an
// unexported type so nothing outside this package can collide with it.
type sentencesKey struct{}

// WithSentences returns a context carrying the application's Sentences. A nil
// one is not stored, so the context answers as one that never had any.
func WithSentences(parent context.Context, s Sentences) context.Context {
	if s == nil {
		return parent
	}
	return context.WithValue(parent, sentencesKey{}, s)
}

// Sentence answers the sentence a person reads for status on r: the one the
// application's Sentences chose, or fallback when it has none or chose none.
//
// Every refusal the collection writes asks it -- Refuse, the problem
// documents, the status pages -- so one Sentences reaches every one of them.
func Sentence(r *http.Request, status int, fallback string) string {
	if r == nil {
		return fallback
	}
	s, ok := r.Context().Value(sentencesKey{}).(Sentences)
	if !ok {
		return fallback
	}
	if chosen := s(r, status); chosen != "" {
		return chosen
	}
	return fallback
}

// LocalizeSentences is the middleware that puts the application's Sentences on
// every request.
//
// Mount it first, before Recover: a refusal answered by the exception handler
// reads the request Recover was handed, and a Sentences put on the context
// further in never reaches it.
func LocalizeSentences(s Sentences) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithSentences(r.Context(), s)))
		})
	}
}
