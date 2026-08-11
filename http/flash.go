package http

// Old answers to Request::old: the value that was typed into the form that was
// rejected, so the page it was sent back to can fill the boxes again. With no
// key, returns all of the old input; with a key, returns the value at that key
// or the default.
//
// This is the method the auditor named as central: it is what makes a form
// come back pre-filled after a rejection, and its pair with
// RedirectResponse::WithInput is the two ends of one thing. WithInput writes
// into the session's old input; Old reads it. If the two diverge, the form
// comes back blank, which is the bug this exists to not have.
//
// Returns the default when no session is set, the way the PHP does.
func (r *Request) Old(key string, def ...any) any {
	if r.session == nil {
		if len(def) > 0 {
			return def[0]
		}
		return nil
	}
	value := r.session.GetOldInput(key, def...)
	if value == nil && len(def) > 0 {
		return def[0]
	}
	return value
}

// Flash answers to Request::flash: flash the request's input to the session,
// so the next request can read it as old input.
//
// Panics when no session is set, as the PHP throws RuntimeException. A handler
// that calls this is inside a flow that started with a session, and a missing
// one there is a wiring error the caller should see immediately.
func (r *Request) Flash() {
	r.Session().FlashInput(r.inputMap())
}

// FlashOnly answers to Request::flashOnly: flash only some of the input.
func (r *Request) FlashOnly(keys ...string) {
	r.Session().FlashInput(r.Only(keys...))
}

// FlashExcept answers to Request::flashExcept: flash the input except the
// given keys. The password and password_confirmation fields are what this is
// for: they should never come back pre-filled.
func (r *Request) FlashExcept(keys ...string) {
	r.Session().FlashInput(r.Except(keys...))
}

// Flush answers to Request::flush: remove all of the old input from the
// session.
func (r *Request) Flush() {
	r.Session().FlashInput(map[string]any{})
}
