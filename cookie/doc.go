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
// CookieServiceProvider.php is not mirrored. It registers the jar in the
// container and reads config, which ADR 0001 and ADR 0002 rejected: an
// application builds its own jar with [NewCookieJar] and
// [CookieJar.SetDefaultPathAndDomain].
//
// The middleware lives in cookie/middleware, as it does in PHP, and it is what
// makes the queue useful: a handler calls [CookieJarFrom] and queues, and the
// response is written for it on the way out.
package cookie
