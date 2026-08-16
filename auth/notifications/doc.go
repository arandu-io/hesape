// Package notifications holds the two messages every application sends and
// nobody wants to write: the password reset link, [ResetPassword], and the
// address confirmation link, [VerifyEmail].
//
// Both are hesape/notifications notifications -- they embed NotificationBase,
// answer Key and Via, and build a messages.Mail from ToMail -- so the Notifier
// delivers them like any other.
//
// # The two callbacks are methods on the zero value
//
// CreateUrlUsing and ToMailUsing set package state, one pair per notification.
// Go has one namespace per package, so two package-level functions could not
// both be called CreateUrlUsing; each is a method with a value receiver
// instead:
//
//	notifications.ResetPassword{}.CreateUrlUsing(func(to hnotifications.Notifiable, token string) string {
//		return "https://app.example.com/reset-password/" + token
//	})
//
// It reads at the call site as the setting it is, and it is found under its own
// type in the reference. The state behind it is guarded by a mutex, because a
// package-level variable written at boot and read by every request is a data
// race the moment a test sets one.
//
// # The URL is the application's, and there is no default worth trusting
//
// Neither the router nor the application key is reachable from here, so the
// built-in URLs are PATHS: no host, and no signature.
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
// The strings are English, and NotificationBase.Locale is what carries the
// locale the recipient asked for to the channel that renders.
package notifications
