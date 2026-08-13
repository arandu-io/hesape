// Package http mirrors Illuminate\Http.
//
// The files it answers to, in the clone at
// laravel_illuminate/http:
//
//	Request.php                            -> request.go, input.go
//	Concerns/InteractsWithInput.php        -> input.go
//	Concerns/InteractsWithContentTypes.php -> content.go
//	Concerns/InteractsWithFlashData.php    -> flash.go
//	Concerns/CanBePrecognitive.php         -> request.go
//	File.php, FileHelpers.php              -> file.go, uploaded_file.go
//	JsonResponse.php                       -> json_response.go
//	RedirectResponse.php                   -> redirect_response.go
//	Response.php, ResponseTrait.php        -> response.go
//	UploadedFile.php                       -> uploaded_file.go
//
// Sub-packages:
//
//	http/client     -> Illuminate\Http\Client
//	http/exceptions -> Illuminate\Http\Exceptions
//	http/middleware -> Illuminate\Http\Middleware
//	http/resources  -> Illuminate\Http\Resources
//	http/testing    -> Illuminate\Http\Testing
//
// # Not mirrored, and why (ADR 0044)
//
//	Request::offsetExists   are PHP's ArrayAccess, the interface behind
//	Request::offsetGet      $request['email']. Go has no operator to
//	Request::offsetSet      overload; Input, Has and Merge are the four
//	Request::offsetUnset    methods under those names, and they are here.
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
