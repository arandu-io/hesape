// Package encryption mirrors Illuminate\Encryption.
//
// The files it answers to, in the clone at
// laravel_illuminate/encryption:
//
//	Encrypter.php                  -- [Encrypter], and the package functions
//	                                  [Encrypt], [Decrypt], [Supported],
//	                                  [AppearsEncrypted]
//	EncryptionServiceProvider.php  -- not ported; ADR 0001 and ADR 0002 reject
//	                                  the container, the facade and the service
//	                                  provider. What it does that is not wiring
//	                                  -- reading APP_KEY and decoding the
//	                                  "base64:" prefix -- is [ParseKey], and
//	                                  config.Load calls it
//	MissingAppKeyException.php     -- not ported; it exists only for the
//	                                  provider above. config.Load reports a
//	                                  missing APP_KEY, naming the variable and
//	                                  the command that fixes it
//
// [Signer] has no counterpart in Illuminate\Encryption. It is the signing half
// of Illuminate\Routing\UrlGenerator::signedRoute and of the password reset
// token, kept here because it signs with the same application key and a second
// place holding that key is a second place to get it wrong.
package encryption
