// Package events mirrors Illuminate\Auth\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Events:
//
//	Attempting.php            -> [Attempting]
//	Authenticated.php         -> [Authenticated]
//	CurrentDeviceLogout.php   -> [CurrentDeviceLogout]
//	Failed.php                -> [Failed]
//	Lockout.php               -> [Lockout]
//	Login.php                 -> [Login]
//	Logout.php                -> [Logout]
//	OtherDeviceLogout.php     -> [OtherDeviceLogout]
//	PasswordReset.php         -> [PasswordReset]
//	PasswordResetLinkSent.php -> [PasswordResetLinkSent]
//	Registered.php            -> [Registered]
//	Validated.php             -> [Validated]
//	Verified.php              -> [Verified]
//
// Each event is a struct whose fields are the arguments the PHP constructor
// takes, with the initial letter upper case (ADR 0044). There is no constructor
// function: a PHP promoted-property constructor and a Go composite literal say
// the same thing, and a New for each of thirteen structs would be thirteen
// functions nobody can get wrong in a way the literal can.
//
// # What is not here, and why
//
// The SerializesModels trait, which nine of the thirteen use. It exists so that
// a queued listener stores the model's key rather than the whole row and reloads
// it on the worker -- a mitigation for PHP serialising an entire object graph
// into the payload. hesape/queue encodes a job as JSON from the fields it was
// given, so there is nothing to trim (RULE 7: the Laravel surface that remedies
// a dynamic-language hazard is not ported).
//
// The #[\SensitiveParameter] attribute on Attempting.Credentials and
// Failed.Credentials, which stops PHP printing the value in a stack trace. Go
// has no such attribute, and the fields carry the warning in their own doc
// comment instead: whoever logs one of these events logs a password.
package events
