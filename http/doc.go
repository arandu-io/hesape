// Package http mirrors Illuminate\Http.
//
// The files it answers to, in the clone at
// laravel_illuminate/http:
//
//	Request.php                  -> request.go, input.go
//	Concerns/InteractsWithInput.php   -> input.go
//	Concerns/InteractsWithContentTypes.php -> content.go
//	Concerns/InteractsWithFlashData.php -> flash.go
//	Concerns/CanBePrecognitive.php  -> request.go
//	File.php, FileHelpers.php      -> (Response fatia)
//	JsonResponse.php               -> (Response fatia)
//	RedirectResponse.php            -> (Response fatia)
//	Response.php, ResponseTrait.php -> (Response fatia)
//	StreamedEvent.php               -> (Response fatia)
//	UploadedFile.php                -> (Response fatia)
//
// What this package implements is the Request half of Illuminate\Http: the
// methods a controller action reaches for to read what the browser sent.
// The Response half -- the methods that write the answer -- is the Response
// fatia, written in parallel.
//
// # Source used
//
// laravel_illuminate/http/Request.php, laravel_illuminate/http/Concerns/*.php,
// and laravel_illuminate/support/Traits/InteractsWithData.php for the trait
// InteractsWithInput leans on.
//
// # The tenant never comes from the request (REGRA 14)
//
// There is no method on Request that reads a tenant id out of a path
// parameter, a query string, a header or a body field, and adding one would
// be the most direct route to a cross-tenant leak. The tenant is on the
// auth.Grant the policy mints, and the repository reads it from there.
//
// If a method here seems to offer tenant access -- Server, Header, Input --
// it does not. They are here because the PHP has them, and they read what
// the browser sent; the tenant is what the Grant authorises, never what the
// request carries.
//
// # Net/http and the package name (ADR 0047)
//
// The package is named http because it mirrors Illuminate\Http, and the
// directory is the component name. When it collides with net/http, the cost
// of the alias is on the caller:
//
//	import (
//	    "net/http"
//	    hhttp "github.com/arandu-io/hesape/http"
//	)
//
// Inside this package, net/http is imported as stdhttp.
package http
