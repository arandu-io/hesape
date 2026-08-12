// Package notifications mirrors Illuminate\Auth\Notifications.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Notifications:
//
//	ResetPassword.php -> [ResetPassword]
//	VerifyEmail.php   -> [VerifyEmail]
//
// The two messages every application sends and nobody wants to write: the
// password reset link and the address confirmation link. Both are
// hesape/notifications notifications -- they embed NotificationBase, answer Key
// and Via, and build a messages.Mail from ToMail -- so the Notifier delivers them
// like any other.
//
// # PHP statics became methods on the zero value
//
// createUrlUsing and toMailUsing are static on both classes. Go has one
// namespace per package, so two package-level functions cannot both be called
// CreateUrlUsing. Keeping Illuminate's name is the rule (ADR 0044), so each is a
// method with a value receiver that writes package state:
//
//	notifications.ResetPassword{}.CreateUrlUsing(func(to hnotifications.Notifiable, token string) string {
//		return "https://app.example.com/reset-password/" + token
//	})
//
// It reads as the static it is, it is found under its type in the reference the
// way a PHP static is found under its class, and the name is unchanged. The
// state behind it is guarded by a mutex, because a package-level variable
// written at boot and read by every request is a data race the moment a test
// sets one.
//
// # The URL is the application's, and there is no default worth trusting
//
// PHP builds the reset URL from route('password.reset') and the verification URL
// from URL::temporarySignedRoute, which is signed with the application key.
// Neither the router nor the key is reachable from here, so the built-in URLs are
// the PATHS those two routes have in a Laravel application, with no host and no
// signature.
//
// A path fails messages.Mail.Validate -- a mail client has no host to resolve one
// against -- so a project that never calls CreateUrlUsing gets a loud failure at
// the send rather than a link nobody can follow. That is deliberate: the
// alternative was a default that half works.
//
// A verification link MUST be signed. Without a signature the link is
// id-plus-hash, and hash is a digest of the address being confirmed -- so
// anybody who knows somebody's e-mail address can confirm it for them. Pass a
// signed URL through CreateUrlUsing; hesape/encryption.Signer is what mints one.
//
// # What is skipped, and why
//
//   - Lang::get around every line. It is the translation facade, and there are no
//     facades (ADR 0002). The strings are English, which is the language of the
//     framework (RULE 6), and NotificationBase.Locale carries what the recipient
//     asked for to the channel that renders.
//   - Notification::via returning a string as well as an array. PHP allows both;
//     hesape/notifications has one shape, a slice (RULE 9).
package notifications
