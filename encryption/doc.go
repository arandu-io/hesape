// Package encryption mirrors Illuminate\Encryption.
//
// The files it answers to, in the clone at
// laravel_illuminate/encryption:
//
//	Encrypter.php                  -- [Encrypter], and the package functions
//	                                  [Encrypt], [Decrypt], [Supported],
//	                                  [AppearsEncrypted]
//	EncryptionServiceProvider.php  -- parseKey and key are [ParseKey], which
//	                                  config.Load calls
//	MissingAppKeyException.php     -- [ErrMissingAppKey], returned by [ParseKey]
//	                                  for the empty key
//
// # What is not ported, and why
//
// EncryptionServiceProvider::register is the only public method of the
// component that is absent. Its body is two container bindings -- singleton
// 'encrypter' and the SerializableClosure secret -- and it is reason 2 of the
// porting rule: a method that exists only to serve the container, the facade
// and the service provider, all three of which ADR 0001 and ADR 0002 rejected.
// Everything register does that is not wiring is [ParseKey], and config.Load
// calls it at boot. There is nothing here for a caller to reach for.
//
// [Signer] has no counterpart in Illuminate\Encryption. It is the signing half
// of Illuminate\Routing\UrlGenerator::signedRoute and of the password reset
// token, kept here because it signs with the same application key and a second
// place holding that key is a second place to get it wrong.
package encryption
