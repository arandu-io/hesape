package http

import (
	"context"

	"github.com/arandu-io/hesape/session"
)

// StatusKey is the key a form that worked leaves its sentence under, the one a
// layout draws above the page it lands on.
const StatusKey = "status"

// flashKey is the context key of the flash a request writes its messages to,
// of an unexported type for the same reason as stateKey.
type flashKey struct{}

// WithFlash returns a context carrying the flash that Context.With writes to.
//
// The router that answers rejected forms calls it with the flash it answers
// them with, so the sentence of a form that worked and the messages of one that
// did not travel in one cookie, signed with one key, read back by one
// middleware. It is exported for that router, and for the test that drives an
// action past it.
func WithFlash(parent context.Context, f *session.Flash) context.Context {
	if f == nil {
		return parent
	}
	return context.WithValue(parent, flashKey{}, f)
}

// FlashFrom returns the flash on a request context, and whether there is one.
func FlashFrom(ctx context.Context) (*session.Flash, bool) {
	f, ok := ctx.Value(flashKey{}).(*session.Flash)
	return f, ok && f != nil
}

// With flashes a sentence for the page this request redirects to, which reads
// it once: the next page view shows it and the one after does not.
//
//	return ctx.With(hhttp.StatusKey, "Profile saved.").RedirectRoute("profile.show")
//
// It is how a form that worked says so. The action writes, then redirects to
// the page that shows the result -- post, redirect, get -- so a reload reads
// the page again instead of posting the form again, and the page shows what
// happened because this carried it across the redirect. The page reads it as
// view.Page.Flashed(key); a layout that draws Flashed(StatusKey) above every
// page gives every form its confirmation without a controller passing it.
//
// The key names the sentence, and "status" -- StatusKey -- is the one a form
// that worked uses. A second call adds to the first. The sentence is text for
// a person, cut to the length of one, and never a value a page computes from:
// that is the controller filling the page's struct.
//
// It does nothing when the request carries no flash, which is a router built
// without one, as in a test: the write the action made stands, and only the
// sentence about it is not shown. A refused request does not call it; it
// returns its validation.Errors, and the router flashes those instead.
func (c *Context) With(key, message string) *Context {
	f, ok := FlashFrom(c.Ctx())
	if !ok || key == "" || message == "" {
		return c
	}
	if c.messages == nil {
		c.messages = &flashedMessages{byKey: map[string]string{}}
	}
	c.messages.byKey[key] = message
	f.WriteMessages(c.Response, c.messages.byKey)
	return c
}

// flashedMessages is what With has written on one response, behind a pointer
// on Context.
type flashedMessages struct {
	byKey map[string]string
}
