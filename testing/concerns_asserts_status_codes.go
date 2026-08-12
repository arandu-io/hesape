package testing

import "net/http"

// This file answers to Illuminate\Testing\Concerns\AssertsStatusCodes.
//
// The PHP is a trait mixed into TestResponse. Go has no traits, and a trait
// method returns $this -- which an embedded Go struct cannot do -- so the
// methods are on TestResponse itself and the file keeps the trait's name.
//
// Every one of them is one call to AssertStatus. They exist because
// AssertStatus(403) says a number and AssertForbidden says what the number
// means, and the failure a test reads at three in the morning is the second
// one.

// AssertOK answers to AssertsStatusCodes::assertOk: 200.
func (r *TestResponse) AssertOK() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusOK)
}

// AssertCreated answers to AssertsStatusCodes::assertCreated: 201.
func (r *TestResponse) AssertCreated() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusCreated)
}

// AssertAccepted answers to AssertsStatusCodes::assertAccepted: 202.
func (r *TestResponse) AssertAccepted() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusAccepted)
}

// AssertNoContent answers to AssertsStatusCodes::assertNoContent: the status,
// 204 unless another is given, and an empty body.
//
// The variadic status stands for the PHP's `$status = 204`.
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

// AssertMovedPermanently answers to
// AssertsStatusCodes::assertMovedPermanently: 301.
func (r *TestResponse) AssertMovedPermanently() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusMovedPermanently)
}

// AssertFound answers to AssertsStatusCodes::assertFound: 302.
func (r *TestResponse) AssertFound() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusFound)
}

// AssertNotModified answers to AssertsStatusCodes::assertNotModified: 304.
func (r *TestResponse) AssertNotModified() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotModified)
}

// AssertTemporaryRedirect answers to
// AssertsStatusCodes::assertTemporaryRedirect: 307.
func (r *TestResponse) AssertTemporaryRedirect() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusTemporaryRedirect)
}

// AssertPermanentRedirect answers to
// AssertsStatusCodes::assertPermanentRedirect: 308.
func (r *TestResponse) AssertPermanentRedirect() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusPermanentRedirect)
}

// AssertBadRequest answers to AssertsStatusCodes::assertBadRequest: 400.
func (r *TestResponse) AssertBadRequest() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusBadRequest)
}

// AssertUnauthorized answers to AssertsStatusCodes::assertUnauthorized: 401.
func (r *TestResponse) AssertUnauthorized() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnauthorized)
}

// AssertPaymentRequired answers to
// AssertsStatusCodes::assertPaymentRequired: 402.
func (r *TestResponse) AssertPaymentRequired() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusPaymentRequired)
}

// AssertForbidden answers to AssertsStatusCodes::assertForbidden: 403.
func (r *TestResponse) AssertForbidden() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusForbidden)
}

// AssertNotFound answers to AssertsStatusCodes::assertNotFound: 404.
func (r *TestResponse) AssertNotFound() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotFound)
}

// AssertMethodNotAllowed answers to
// AssertsStatusCodes::assertMethodNotAllowed: 405.
func (r *TestResponse) AssertMethodNotAllowed() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusMethodNotAllowed)
}

// AssertNotAcceptable answers to AssertsStatusCodes::assertNotAcceptable: 406.
func (r *TestResponse) AssertNotAcceptable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusNotAcceptable)
}

// AssertRequestTimeout answers to AssertsStatusCodes::assertRequestTimeout: 408.
func (r *TestResponse) AssertRequestTimeout() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusRequestTimeout)
}

// AssertConflict answers to AssertsStatusCodes::assertConflict: 409.
func (r *TestResponse) AssertConflict() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusConflict)
}

// AssertGone answers to AssertsStatusCodes::assertGone: 410.
func (r *TestResponse) AssertGone() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusGone)
}

// AssertUnsupportedMediaType answers to
// AssertsStatusCodes::assertUnsupportedMediaType: 415.
func (r *TestResponse) AssertUnsupportedMediaType() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnsupportedMediaType)
}

// AssertUnprocessable answers to AssertsStatusCodes::assertUnprocessable: 422.
func (r *TestResponse) AssertUnprocessable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusUnprocessableEntity)
}

// AssertTooManyRequests answers to
// AssertsStatusCodes::assertTooManyRequests: 429.
func (r *TestResponse) AssertTooManyRequests() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusTooManyRequests)
}

// AssertInternalServerError answers to
// AssertsStatusCodes::assertInternalServerError: 500.
func (r *TestResponse) AssertInternalServerError() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusInternalServerError)
}

// AssertServiceUnavailable answers to
// AssertsStatusCodes::assertServiceUnavailable: 503.
func (r *TestResponse) AssertServiceUnavailable() *TestResponse {
	r.t.Helper()
	return r.AssertStatus(http.StatusServiceUnavailable)
}
