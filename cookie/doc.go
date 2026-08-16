// Package cookie holds a [CookieJar]: cookies built with the application's
// defaults, and a queue that the middleware in cookie/middleware flushes
// onto the response on its way out. A handler calls [CookieJarFrom] to reach
// the jar for the current request and queues a cookie without ever touching
// the http.ResponseWriter.
//
// [CookieValuePrefix] binds a cookie's value to the name it was issued
// under, so a value stolen from one cookie cannot be replayed under
// another: the prefix is a keyed MAC over the name, checked and stripped
// before the value is used. Its three methods are reached through the
// [CookieValuePrefix] value rather than written as package-level functions,
// because naming them at the package level would say nothing about what
// they create, remove or validate.
package cookie
