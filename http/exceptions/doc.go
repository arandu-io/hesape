// Package exceptions mirrors Illuminate\Http\Exceptions.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Exceptions:
//
//	HttpResponseException.php   -> exceptions.go
//	MalformedUrlException.php   -> exceptions.go
//	OriginMismatchException.php -> exceptions.go
//	PostTooLargeException.php   -> exceptions.go
//	ThrottleRequestsException.php -> exceptions.go
//
// # Constructors, and the class hierarchy Go does not have
//
// Every PHP __construct is a NewX function: Go has no constructors, and the
// zero value of these types would be a 0 status, which is not an answer.
//
// PostTooLargeException, ThrottleRequestsException and MalformedUrlException
// all extend Symfony's HttpException in the PHP. Go has no class inheritance,
// so [HTTPError] is a struct they embed, and errors.As reaches it through any
// of them.
package exceptions
