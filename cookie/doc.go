// Package cookie mirrors Illuminate\Cookie.
//
// The files it answers to, in the clone at laravel_illuminate/cookie:
//
//	CookieJar.php
//	CookieValuePrefix.php
//
// CookieJar::queued() is written from the copy in
// reference_laravel/framework/src/Illuminate/Cookie/CookieJar.php: the clone
// reaches for the name with Arr::get($this->queued, $key, $default) and then
// runs Arr::last over whatever came back, so a missing name walks the default
// as if it were an array. The current release reads the name with ?? and
// returns the default straight, which is the same intent said once. Everything
// else in the package is identical in both copies.
//
// CookieServiceProvider.php is not mirrored, and CookieServiceProvider::register
// is the one method of this component with no answer here -- reason 2 of ADR
// 0044, a method that only serves the container, a facade or a service provider.
// Its body binds a CookieJar as the 'cookie' singleton and calls
// setDefaultPathAndDomain with four keys read out of the session configuration,
// which ADR 0001 and ADR 0002 rejected. What is written here instead is those
// same two calls, in the application's own wiring and with the four values in
// sight: [NewCookieJar] and [CookieJar.SetDefaultPathAndDomain].
//
// The three methods of CookieValuePrefix are static, and they are reached
// through the [CookieValuePrefix] value rather than as package functions, so
// that the call keeps the name the PHP gives it:
//
//	CookieValuePrefix::create($name, $key)     // PHP
//	cookie.CookieValuePrefix.Create(name, key) // Go
//
// Naming them CreateCookieValuePrefix and the rest would have been three names
// this framework invented, which ADR 0044 does not allow; naming them Create,
// Remove and Validate at the package level would have said nothing about what
// they create, remove or validate.
//
// The middleware lives in cookie/middleware, as it does in PHP, and it is what
// makes the queue useful: a handler calls [CookieJarFrom] and queues, and the
// response is written for it on the way out.
package cookie
