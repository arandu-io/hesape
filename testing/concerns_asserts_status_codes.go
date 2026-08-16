package testing

import "net/http"

// The status assertions on [TestResponse].
//
// Every one of them is one call to [TestResponse.AssertStatus]. They exist
// because AssertStatus(403) says a number and AssertForbidden says what the
// number means, and the failure a test reads at three in the morning is the
// second one.

// AssertOK asserts a 200 status.
func (r *TestResponse) AssertOK() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusOK)
}

// AssertCreated asserts a 201 status.
func (r *TestResponse) AssertCreated() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusCreated)
}

// AssertAccepted asserts a 202 status.
func (r *TestResponse) AssertAccepted() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusAccepted)
}

// AssertNoContent asserts the status -- 204 unless another is given -- and an
// empty body.
func (r *TestResponse) AssertNoContent(status ...int) *TestResponse {
	r.t.Helper()

	code := http.StatusNoContent
	if len(status) > 0 {
		code = status[0]
	}

	r.AssertStatus(code)
	assertEmpty(r.t, r.GetContent(), "Response content is not empty.")
	return r
}

// AssertMovedPermanently asserts a 301 status.
func (r *TestResponse) AssertMovedPermanently() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusMovedPermanently)
}

// AssertFound asserts a 302 status.
func (r *TestResponse) AssertFound() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusFound)
}

// AssertNotModified asserts a 304 status.
func (r *TestResponse) AssertNotModified() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotModified)
}

// AssertTemporaryRedirect asserts a 307 status.
func (r *TestResponse) AssertTemporaryRedirect() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusTemporaryRedirect)
}

// AssertPermanentRedirect asserts a 308 status.
func (r *TestResponse) AssertPermanentRedirect() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusPermanentRedirect)
}

// AssertBadRequest asserts a 400 status.
func (r *TestResponse) AssertBadRequest() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusBadRequest)
}

// AssertUnauthorized asserts a 401 status.
func (r *TestResponse) AssertUnauthorized() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnauthorized)
}

// AssertPaymentRequired asserts a 402 status.
func (r *TestResponse) AssertPaymentRequired() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusPaymentRequired)
}

// AssertForbidden asserts a 403 status.
func (r *TestResponse) AssertForbidden() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusForbidden)
}

// AssertNotFound asserts a 404 status.
func (r *TestResponse) AssertNotFound() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotFound)
}

// AssertMethodNotAllowed asserts a 405 status.
func (r *TestResponse) AssertMethodNotAllowed() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusMethodNotAllowed)
}

// AssertNotAcceptable asserts a 406 status.
func (r *TestResponse) AssertNotAcceptable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotAcceptable)
}

// AssertRequestTimeout asserts a 408 status.
func (r *TestResponse) AssertRequestTimeout() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusRequestTimeout)
}

// AssertConflict asserts a 409 status.
func (r *TestResponse) AssertConflict() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusConflict)
}

// AssertGone asserts a 410 status.
func (r *TestResponse) AssertGone() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusGone)
}

// AssertUnsupportedMediaType asserts a 415 status.
func (r *TestResponse) AssertUnsupportedMediaType() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnsupportedMediaType)
}

// AssertUnprocessable asserts a 422 status.
func (r *TestResponse) AssertUnprocessable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnprocessableEntity)
}

// AssertTooManyRequests asserts a 429 status.
func (r *TestResponse) AssertTooManyRequests() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusTooManyRequests)
}

// AssertInternalServerError asserts a 500 status.
func (r *TestResponse) AssertInternalServerError() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusInternalServerError)
}

// AssertServiceUnavailable asserts a 503 status.
func (r *TestResponse) AssertServiceUnavailable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusServiceUnavailable)
}
