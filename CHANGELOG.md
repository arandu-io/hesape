# Release Notes

## [v0.106.0](https://github.com/arandu-io/hesape/compare/v0.105.2...v0.106.0) - 2026-11-14

- **support: `Brand`** is the application's name as a visitor's browser is given it: the prefix of its cookies, of the keys its scripts keep in `localStorage` and of the address its static files and token refresh are served under. `support.NewBrand("Peráta")` is `perata` (accents folded, anything that is not a letter or a digit becomes `_`), `Cookie("session")` is `perata_session`, `StorageKey("theme")` is `perata.theme` and `Path("assets/")` is `/_perata/assets/`. One value is passed at the wiring to everything below, instead of one setting per item, so the part that writes something and the part that reads it cannot disagree. The zero `Brand` is the default one, named `arandu` as before: an application that configures nothing is not changed.
- **session: `CookieNames`, `CookieNamesFor(brand)` and `WithCookieNames`** name the four cookies the framework writes: session, CSRF guest, flash and intended address. `NewRecordStore`, `NewCSRF`, `NewFlash`, `http.NewIntended` and `http.NewSessionStore` take the option as a trailing variadic argument, so the call that passes none is the call every application already writes and keeps `arandu_session`, `arandu_csrf_guest`, `arandu_flash` and `arandu_intended`. `RecordStore`, `CSRF`, `Flash`, `Intended` and `SessionStore` answer the name they use (`CookieName`, `GuestCookieName`, `CookieNames`), and `TestResponse` reads the flash under the flash's own name. The session signature covers the cookie name, so a cookie copied under another name is not a session. `CookieName`, `GuestCookieName`, `FlashCookieName` and `IntendedCookieName` stay, as the names of a store given no others, and their comments no longer say they are fixed.
- **view: `UseBrand(brand)`** serves the assets under the brand's own prefix (`/_perata/assets/`), writes the brand into `theme.js` (`perata.theme`) and `ui.js` (`perata.sidebar.<id>` and the token refresh at `/_perata/csrf`), and rehashes them. `Asset`, `Assets` and the module route follow it; `Handler` answers the old `/_arandu/assets/` prefix as well, uncached because the old hash names a body that no longer exists, so a page or an e-mail written before the change still gets its file. A preference saved under the old `localStorage` key is not read: each visitor is offered the default theme and sidebar once. `AssetPrefix()` answers the prefix in force.
- **middleware: `CSRFRefreshUnder(brand)`** makes `CSRFProtect` answer the token refresh at `/_perata/csrf` as well as at `CSRFRefreshPath`, which keeps answering.
- **http, middleware: the CSRF token is issued when something asks for it.** `CSRFProtect` no longer issues a token, and so no longer writes the guest cookie, on every GET. It puts an issuer on the request context (`http.WithCSRFTokenIssuer`), and `CSRFTokenFrom` -- which `view.New` calls while a page is built -- issues the token the first time it is asked. A response that draws no form carries no cookie: an API call that answers 401 no longer sets a guest binding in a browser that will never use it. The response that does ask is marked `Cache-Control: no-store, private` unless the handler already set a policy of its own, so a sign-in, sign-up or password-recovery form is not kept by a cache between people nor offered again by the back button. Read the token before the first byte of the response is written: a cookie set afterwards is lost. `WithCSRFToken` is unchanged.

**What changes for an application that does not pass a name.** Nothing in the names; the token, the cookie and the cache header behave as described above. **For one that does** -- the framework passes the brand of `APP_NAME` from its next release -- every cookie, key and asset address changes once: sessions end and visitors sign in again, and the saved theme and sidebar are forgotten.

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.105.2...v0.106.0

## [v0.105.2](https://github.com/arandu-io/hesape/compare/v0.105.1...v0.105.2) - 2026-11-14

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.105.1...v0.105.2

## [v0.105.1](https://github.com/arandu-io/hesape/compare/v0.105.0...v0.105.1) - 2026-11-14

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.105.0...v0.105.1

## [v0.105.0](https://github.com/arandu-io/hesape/compare/v0.104.0...v0.105.0) - 2026-11-13

- **validation: `CommonPasswords()`** is an `UncompromisedVerifier` that refuses the passwords everybody tries first, with no network and no setup: `validation.PasswordMin(12).Max(128).Uncompromised(validation.CommonPasswords())`. It reads the SecLists 10k-most-common list (MIT, notice in `validation/THIRD_PARTY.md`) and a few hundred passwords common in Brazil, both embedded in the binary, and it compares the shape of the password and not only the text, because a minimum of twelve characters leaves no list entry that long: another case, a trailing or leading run of digits and symbols (`Password1234`, `Senha@2026!`), leetspeak and accents folded (`p@ssw0rd`), a word or a short unit repeated (`passwordpassword`, `abababababab`), one repeated character, an ascending or descending run (`123456789012`, `abcdefghijkl`) and a walk across the keyboard (`qwertyuiopas`, `1q2w3e4r5t6y`), alone or chained. A passphrase of unrelated words and a random password pass. The threshold is ignored. It is stateless, safe for concurrent use and allocates nothing for a password of up to 128 runes.
- **validation: `UncompromisedReason`** is the optional interface of a verifier that refuses for a reason other than a leak: `Refusal()` returns the translation key and the English fallback, and `Password.Uncompromised` says that sentence instead of "has appeared in a data leak". `CommonPasswords` uses the new line `validation.password.common`, shipped in `en` and `pt-BR`. A verifier that does not implement it is refused with the leak sentence as before, and `AppliedRules()` still reports `uncompromised: true` for any verifier.
- **auth: `ThrottlePolicy.IdentityWide`.** When true, the identity's budget (`MaxFailures`) is keyed by tenant and identity alone and is spent by every address, instead of by tenant, identity and address. `MaxFailuresPerClient` is unchanged, and so is `LockoutPolicy()`: an application that wants ten failures from any address to lock an account for thirty minutes writes `p := auth.LockoutPolicy(); p.IdentityWide = true`. `MemoryThrottle` honours it, and its `Clear` forgets what every address spent on the identity.
- **cache: `NewSignInThrottle(store, policy)`** is `auth.SignInThrottle` over any store that has `Store` and `Locking` -- the Redis store, the database store, the array store -- through the new `ThrottleStore` interface, so the lockout survives a restart and is the same on every replica. `Attempt` takes the store's lock on the identity's counter and the address's, sorted by key, reads both, decides and writes both with their expiry, as one step; a burst of 200 simultaneous guesses against a budget of 10 lets exactly 10 through. The keys are SHA-256 hashes under `arandu:signin-throttle:v1:`, with the tenant in each, so no e-mail or address is readable in Redis. If the locks cannot be taken within `LockWait` (750 ms by default) that one attempt is refused with a one-second `retryAfter` and counts for nothing. If the store returns an error the call degrades to an in-process `auth.MemoryThrottle` with the same policy and a warning in the log, at most once a minute. `Refund` and `Clear` finish under their own three-second bound even when the request's context was already cancelled, which is when `Refund` is called; every other call honours its context.
- **auth: the comments that promised a Redis-backed throttle** now point at `cache.NewSignInThrottle`, which is the one that exists.

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.104.0...v0.105.0

## [v0.104.0](https://github.com/arandu-io/hesape/compare/v0.103.0...v0.104.0) - 2026-11-10

- **webhook: relay.** `Module.Relay(ctx, g, endpointID, RelayMessage{...})` forwards a request the application received to an endpoint of the new `DialectRelay` as it arrived: the same method (POST or PUT), the same `Content-Type`, the same body bytes, the allowlisted headers of the original request (never `Authorization`, `Cookie`, `Host`, `Forwarded`, `X-Forwarded-*`, `CF-*`, or a name `session.IsSecretField` calls secret) and the endpoint's own headers, with no signature and no `X-Arandu` header. An endpoint with `KeepPath` is called at its address plus the original path and query. `Relay` records a `Delivery` and queues its first attempt, in the caller's transaction when it has one, and never waits for the receiving server. The retry policy, the history, the redelivery, the guarded transport and the cleans are the delivery engine's own.
- **webhook: large bodies stay out of the database.** A relayed body above `Server.RelayInlineBytes` (64 KiB by default, as base64 in the row below it) is written to `Server.Bodies`, a `*filesystem.Disk` the application passes, under a key the module generates in the Grant's tenant. The row keeps the key, the size and the SHA-256; every attempt and redelivery streams the same bytes with their length. The clean that removes a settled delivery removes its body first, and removes the bodies older than a day that no delivery holds. Without `Server.Bodies`, `Relay` answers `ErrNoBodyDisk` to a body above the inline room.
- **webhook: `Policy.MaxRelayBytes`** is the limit of a relayed body, 16 MiB in `DefaultPolicy()` and at most `MaxRelayBytes`; zero relays nothing. `MaxRequestBytes` stays 1 MiB for every other delivery. An endpoint's own policy stores it as `max_relay_bytes`.
- **webhook: `EndpointRequest.KeepPath`**, `Endpoint.KeepPath` and the `keep_path` field of the management routes. `Endpoint.Receives` is false for a relay endpoint, `Module.Send` and the test call answer `ErrRelayEndpoint` for one, and `Relay` answers `ErrNotRelayEndpoint` for any other.
- **webhook: migrations** `20261110_0010_add_keep_path_to_webhook_server_endpoints` and `20261110_0011_add_relay_to_webhook_server_deliveries` add columns with defaults and one index; a row written by the previous release reads back as the POST of a JSON body it was. Run them before the binary that relays.

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.103.0...v0.104.0

## [v0.103.0](https://github.com/arandu-io/hesape/compare/v0.102.0...v0.103.0) - 2026-11-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.102.0...v0.103.0

## [v0.102.0](https://github.com/arandu-io/hesape/compare/v0.101.0...v0.102.0) - 2026-11-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.101.0...v0.102.0

## [v0.101.0](https://github.com/arandu-io/hesape/compare/v0.100.0...v0.101.0) - 2026-11-02

- **oauth: Sign in with Apple.** `providers.NewAppleProvider(state, servicesID, teamID, keyID, p8)` is the authorization code flow for people who already have an account: it answers who the person is and creates nothing. The client secret is an ES256 JWT signed with the `.p8` key, made for each exchange and valid for five minutes. The redirect asks for `response_mode=form_post`, so the callback is a cross-site POST: the route accepts POST, is exempted with `middleware.CSRFExcept` and keeps the state check. The person comes from the `id_token`, verified for signature, `iss`, `aud`, expiry and a nonce derived from the state; the answer carries `id`, `sub`, `email`, `email_verified` and `is_private_email` as booleans, and the name Apple posts once on the first authorization.
- **oauth: `providers.VerifyIDToken`** checks an OpenID Connect ID token for the native flow, where Android and iOS hand the token to the app and no redirect happens. `GoogleIssuer()` and `AppleIssuer()` carry the `iss` values and the signing keys; `NewTokenIssuer` builds one for another provider. It accepts RS256 only, refuses an empty audience list, compares in constant time, takes `WithNonce`, and never repeats the token in an error. A fault of the token wraps `ErrInvalidIDToken` (`ErrExpiredIDToken` for age alone); keys that could not be fetched wrap `ErrKeysUnavailable`.
- **oauth: `providers.KeySet`** fetches a provider's JWKS with the guarded client, keeps it for the `max-age` the provider sends (clamped to five minutes through a day), refetches for a key id it has not seen at most once a minute, and keeps serving the old keys for up to a day when the provider is down.
- **oauth: `CookieStateStore.CrossSite()`** keeps the state cookie as `SameSite=None; Secure`, which is what a browser sends on a cross-site POST. `NewAppleProvider` calls it on a `CookieStateStore` it is given.
- **oauth: `UserFromIDToken`** on `Provider` makes the person come from the ID token instead of a user data endpoint, for any OpenID Connect provider.
- **oauth: `UserData.Bool`** reads a flag that a provider writes as a boolean or as the text `true`.
- **oauth: the package comments listed Twitter**, which was never carried; they now list the providers that are.

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.100.0...v0.101.0

## [v0.100.0](https://github.com/arandu-io/hesape/compare/v0.99.0...v0.100.0) - 2026-11-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.99.0...v0.100.0

## [v0.99.0](https://github.com/arandu-io/hesape/compare/v0.98.0...v0.99.0) - 2026-11-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.98.0...v0.99.0

## [v0.98.0](https://github.com/arandu-io/hesape/compare/v0.97.0...v0.98.0) - 2026-11-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.97.0...v0.98.0

## [v0.97.0](https://github.com/arandu-io/hesape/compare/v0.96.0...v0.97.0) - 2026-11-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.96.0...v0.97.0

## [v0.96.0](https://github.com/arandu-io/hesape/compare/v0.95.0...v0.96.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.95.0...v0.96.0

## [v0.95.0](https://github.com/arandu-io/hesape/compare/v0.94.0...v0.95.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.94.0...v0.95.0

## [v0.94.0](https://github.com/arandu-io/hesape/compare/v0.93.0...v0.94.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.93.0...v0.94.0

## [v0.93.0](https://github.com/arandu-io/hesape/compare/v0.92.0...v0.93.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.92.0...v0.93.0

## [v0.92.0](https://github.com/arandu-io/hesape/compare/v0.91.0...v0.92.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.91.0...v0.92.0

## [v0.91.0](https://github.com/arandu-io/hesape/compare/v0.90.0...v0.91.0) - 2026-11-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.90.0...v0.91.0

## [v0.90.0](https://github.com/arandu-io/hesape/compare/v0.89.2...v0.90.0) - 2026-10-31

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.89.2...v0.90.0

## [v0.89.2](https://github.com/arandu-io/hesape/compare/v0.89.1...v0.89.2) - 2026-10-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.89.1...v0.89.2

## [v0.89.1](https://github.com/arandu-io/hesape/compare/v0.89.0...v0.89.1) - 2026-10-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.89.0...v0.89.1

## [v0.89.0](https://github.com/arandu-io/hesape/compare/v0.88.1...v0.89.0) - 2026-10-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.88.1...v0.89.0

## [v0.88.1](https://github.com/arandu-io/hesape/compare/v0.88.0...v0.88.1) - 2026-10-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.88.0...v0.88.1

## [v0.88.0](https://github.com/arandu-io/hesape/compare/v0.87.0...v0.88.0) - 2026-10-25

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.87.0...v0.88.0

## [v0.87.0](https://github.com/arandu-io/hesape/compare/v0.86.0...v0.87.0) - 2026-10-25

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.86.0...v0.87.0

## [v0.86.0](https://github.com/arandu-io/hesape/compare/v0.85.0...v0.86.0) - 2026-10-25

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.85.0...v0.86.0

## [v0.85.0](https://github.com/arandu-io/hesape/compare/v0.84.2...v0.85.0) - 2026-10-25

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.84.2...v0.85.0

## [v0.84.2](https://github.com/arandu-io/hesape/compare/v0.84.1...v0.84.2) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.84.1...v0.84.2

## [v0.84.1](https://github.com/arandu-io/hesape/compare/v0.84.0...v0.84.1) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.84.0...v0.84.1

## [v0.84.0](https://github.com/arandu-io/hesape/compare/v0.83.0...v0.84.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.83.0...v0.84.0

## [v0.83.0](https://github.com/arandu-io/hesape/compare/v0.82.0...v0.83.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.82.0...v0.83.0

## [v0.82.0](https://github.com/arandu-io/hesape/compare/v0.81.0...v0.82.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.81.0...v0.82.0

## [v0.81.0](https://github.com/arandu-io/hesape/compare/v0.80.0...v0.81.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.80.0...v0.81.0

## [v0.80.0](https://github.com/arandu-io/hesape/compare/v0.79.0...v0.80.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.79.0...v0.80.0

## [v0.79.0](https://github.com/arandu-io/hesape/compare/v0.78.0...v0.79.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.78.0...v0.79.0

## [v0.78.0](https://github.com/arandu-io/hesape/compare/v0.77.0...v0.78.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.77.0...v0.78.0

## [v0.77.0](https://github.com/arandu-io/hesape/compare/v0.76.0...v0.77.0) - 2026-10-22

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.76.0...v0.77.0

## [v0.76.0](https://github.com/arandu-io/hesape/compare/v0.75.0...v0.76.0) - 2026-10-22

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.75.0...v0.76.0

## [v0.75.0](https://github.com/arandu-io/hesape/compare/v0.74.0...v0.75.0) - 2026-10-21

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.74.0...v0.75.0

## [v0.74.0](https://github.com/arandu-io/hesape/compare/v0.73.0...v0.74.0) - 2026-10-21

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.73.0...v0.74.0

## [v0.73.0](https://github.com/arandu-io/hesape/compare/v0.72.0...v0.73.0) - 2026-10-21

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.72.0...v0.73.0

## [v0.72.0](https://github.com/arandu-io/hesape/compare/v0.71.0...v0.72.0) - 2026-10-20

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.71.0...v0.72.0

## [v0.71.0](https://github.com/arandu-io/hesape/compare/v0.70.0...v0.71.0) - 2026-10-20

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.70.0...v0.71.0

## [v0.70.0](https://github.com/arandu-io/hesape/compare/v0.69.0...v0.70.0) - 2026-10-20

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.69.0...v0.70.0

## [v0.69.0](https://github.com/arandu-io/hesape/compare/v0.68.0...v0.69.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.68.0...v0.69.0

## [v0.68.0](https://github.com/arandu-io/hesape/compare/v0.67.0...v0.68.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.67.0...v0.68.0

## [v0.67.0](https://github.com/arandu-io/hesape/compare/v0.66.0...v0.67.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.66.0...v0.67.0

## [v0.66.0](https://github.com/arandu-io/hesape/compare/v0.65.0...v0.66.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.65.0...v0.66.0

## [v0.65.0](https://github.com/arandu-io/hesape/compare/v0.64.1...v0.65.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.64.1...v0.65.0

## [v0.64.1](https://github.com/arandu-io/hesape/compare/v0.64.0...v0.64.1) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.64.0...v0.64.1

## [v0.64.0](https://github.com/arandu-io/hesape/compare/v0.63.0...v0.64.0) - 2026-10-19

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.63.0...v0.64.0

## [v0.63.0](https://github.com/arandu-io/hesape/compare/v0.62.0...v0.63.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.62.0...v0.63.0

## [v0.62.0](https://github.com/arandu-io/hesape/compare/v0.61.0...v0.62.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.61.0...v0.62.0

## [v0.61.0](https://github.com/arandu-io/hesape/compare/v0.60.0...v0.61.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.60.0...v0.61.0

## [v0.60.0](https://github.com/arandu-io/hesape/compare/v0.59.0...v0.60.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.59.0...v0.60.0

## [v0.59.0](https://github.com/arandu-io/hesape/compare/v0.58.0...v0.59.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.58.0...v0.59.0

## [v0.58.0](https://github.com/arandu-io/hesape/compare/v0.57.0...v0.58.0) - 2026-10-13

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.57.0...v0.58.0

## [v0.57.0](https://github.com/arandu-io/hesape/compare/v0.56.0...v0.57.0) - 2026-10-13

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.56.0...v0.57.0

## [v0.56.0](https://github.com/arandu-io/hesape/compare/v0.55.0...v0.56.0) - 2026-10-13

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.55.0...v0.56.0

## [v0.55.0](https://github.com/arandu-io/hesape/compare/v0.54.1...v0.55.0) - 2026-10-12

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.54.1...v0.55.0

## [v0.54.0](https://github.com/arandu-io/hesape/compare/v0.53.0...v0.54.0) - 2026-10-10

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.53.0...v0.54.0

## [v0.53.0](https://github.com/arandu-io/hesape/compare/v0.52.0...v0.53.0) - 2026-10-10

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.52.0...v0.53.0

## [v0.52.0](https://github.com/arandu-io/hesape/compare/v0.51.0...v0.52.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.51.0...v0.52.0

## [v0.51.0](https://github.com/arandu-io/hesape/compare/v0.50.3...v0.51.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.50.3...v0.51.0

## [v0.50.3](https://github.com/arandu-io/hesape/compare/v0.50.2...v0.50.3) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.50.2...v0.50.3

## [v0.50.2](https://github.com/arandu-io/hesape/compare/v0.50.1...v0.50.2) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.50.1...v0.50.2

## [v0.50.1](https://github.com/arandu-io/hesape/compare/v0.50.0...v0.50.1) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.50.0...v0.50.1

## [v0.50.0](https://github.com/arandu-io/hesape/compare/v0.49.0...v0.50.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.49.0...v0.50.0

## [v0.48.1](https://github.com/arandu-io/hesape/compare/v0.48.0...v0.48.1) - 2026-10-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.48.0...v0.48.1

## [v0.48.0](https://github.com/arandu-io/hesape/compare/v0.47.0...v0.48.0) - 2026-10-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.47.0...v0.48.0

## [v0.47.0](https://github.com/arandu-io/hesape/compare/v0.46.0...v0.47.0) - 2026-10-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.46.0...v0.47.0

## [v0.46.0](https://github.com/arandu-io/hesape/compare/v0.45.0...v0.46.0) - 2026-10-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.45.0...v0.46.0

## [v0.45.0](https://github.com/arandu-io/hesape/compare/v0.44.3...v0.45.0) - 2026-10-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.44.3...v0.45.0

## [v0.44.3](https://github.com/arandu-io/hesape/compare/v0.44.2...v0.44.3) - 2026-10-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.44.2...v0.44.3

## [v0.44.2](https://github.com/arandu-io/hesape/compare/v0.44.1...v0.44.2) - 2026-10-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.44.1...v0.44.2

## [v0.44.1](https://github.com/arandu-io/hesape/compare/v0.44.0...v0.44.1) - 2026-10-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.44.0...v0.44.1

## [v0.44.0](https://github.com/arandu-io/hesape/compare/v0.43.1...v0.44.0) - 2026-10-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.43.1...v0.44.0

## [v0.43.1](https://github.com/arandu-io/hesape/compare/v0.43.0...v0.43.1) - 2026-09-28

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.43.0...v0.43.1

## [v0.43.0](https://github.com/arandu-io/hesape/compare/v0.42.2...v0.43.0) - 2026-09-28

## What's Changed
* build(deps): bump golang.org/x/crypto from 0.55.0 to 0.57.0 in the gomod group across 1 directory by @dependabot[bot] in https://github.com/arandu-io/hesape/pull/6
* build(deps): bump modernc.org/sqlite from 1.57.0 to 1.58.0 in /database/connectors/sqlite in the gomod group across 1 directory by @dependabot[bot] in https://github.com/arandu-io/hesape/pull/7
* build(deps): bump github.com/go-sql-driver/mysql from 1.10.0 to 1.10.1 in /database/connectors/mysql in the gomod group across 1 directory by @dependabot[bot] in https://github.com/arandu-io/hesape/pull/8
* build(deps): bump github.com/jackc/pgx/v5 from 5.10.0 to 5.11.0 in /database/connectors/pgx in the gomod group by @dependabot[bot] in https://github.com/arandu-io/hesape/pull/9
* build(deps): bump modernc.org/sqlite from 1.58.0 to 1.59.0 in /database/connectors/sqlite in the gomod group by @dependabot[bot] in https://github.com/arandu-io/hesape/pull/10

## New Contributors
* @dependabot[bot] made their first contribution in https://github.com/arandu-io/hesape/pull/6

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.42.2...v0.43.0

## [v0.42.2](https://github.com/arandu-io/hesape/compare/v0.42.1...v0.42.2) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.42.1...v0.42.2

## [v0.42.1](https://github.com/arandu-io/hesape/compare/v0.42.0...v0.42.1) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.42.0...v0.42.1

## [v0.42.0](https://github.com/arandu-io/hesape/compare/v0.41.3...v0.42.0) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.41.3...v0.42.0

## [v0.41.3](https://github.com/arandu-io/hesape/compare/v0.41.2...v0.41.3) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.41.2...v0.41.3

## [v0.41.2](https://github.com/arandu-io/hesape/compare/v0.41.1...v0.41.2) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.41.1...v0.41.2

## [v0.41.1](https://github.com/arandu-io/hesape/compare/v0.41.0...v0.41.1) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.41.0...v0.41.1

## [v0.41.0](https://github.com/arandu-io/hesape/compare/v0.40.0...v0.41.0) - 2026-09-14

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.40.0...v0.41.0

## [v0.40.0](https://github.com/arandu-io/hesape/compare/v0.39.0...v0.40.0) - 2026-09-11

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.39.0...v0.40.0

## [v0.39.0](https://github.com/arandu-io/hesape/compare/v0.38.0...v0.39.0) - 2026-09-10

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.38.0...v0.39.0

## [v0.38.0](https://github.com/arandu-io/hesape/compare/v0.37.0...v0.38.0) - 2026-09-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.37.0...v0.38.0

## [v0.37.0](https://github.com/arandu-io/hesape/compare/v0.36.0...v0.37.0) - 2026-09-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.36.0...v0.37.0

## [v0.36.0](https://github.com/arandu-io/hesape/compare/v0.35.0...v0.36.0) - 2026-09-09

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.35.0...v0.36.0

## [v0.35.0](https://github.com/arandu-io/hesape/compare/v0.34.0...v0.35.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.34.0...v0.35.0

## [v0.34.0](https://github.com/arandu-io/hesape/compare/v0.33.0...v0.34.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.33.0...v0.34.0

## [v0.33.0](https://github.com/arandu-io/hesape/compare/v0.32.0...v0.33.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.32.0...v0.33.0

## [v0.32.0](https://github.com/arandu-io/hesape/compare/v0.31.0...v0.32.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.31.0...v0.32.0

## [v0.31.0](https://github.com/arandu-io/hesape/compare/v0.30.0...v0.31.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.30.0...v0.31.0

## [v0.30.0](https://github.com/arandu-io/hesape/compare/v0.29.0...v0.30.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.29.0...v0.30.0

## [v0.29.0](https://github.com/arandu-io/hesape/compare/v0.28.0...v0.29.0) - 2026-09-07

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.28.0...v0.29.0

## [v0.28.0](https://github.com/arandu-io/hesape/compare/v0.27.0...v0.28.0) - 2026-09-07

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.27.0...v0.28.0

## [v0.27.0](https://github.com/arandu-io/hesape/compare/v0.26.0...v0.27.0) - 2026-09-07

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.26.0...v0.27.0

## [v0.26.0](https://github.com/arandu-io/hesape/compare/v0.25.2...v0.26.0) - 2026-09-06

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.25.2...v0.26.0

## [v0.25.2](https://github.com/arandu-io/hesape/compare/v0.25.1...v0.25.2) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.25.1...v0.25.2

## [v0.25.1](https://github.com/arandu-io/hesape/compare/v0.25.0...v0.25.1) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.25.0...v0.25.1

## [v0.25.0](https://github.com/arandu-io/hesape/compare/v0.24.0...v0.25.0) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.24.0...v0.25.0

## [v0.24.0](https://github.com/arandu-io/hesape/compare/v0.23.0...v0.24.0) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.23.0...v0.24.0

## [v0.22.0](https://github.com/arandu-io/hesape/compare/v0.21.1...v0.22.0) - 2026-09-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.21.1...v0.22.0

## [v0.22.0](https://github.com/arandu-io/hesape/compare/v0.21.1...v0.22.0) - 2026-09-03

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.21.1...v0.22.0

## [v0.21.1](https://github.com/arandu-io/hesape/compare/v0.21.0...v0.21.1) - 2026-09-02

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.21.0...v0.21.1

## [v0.21.0](https://github.com/arandu-io/hesape/compare/v0.20.0...v0.21.0) - 2026-09-01

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.20.0...v0.21.0

## [v0.20.0](https://github.com/arandu-io/hesape/compare/v0.19.1...v0.20.0) - 2026-08-31

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.19.1...v0.20.0

## [v0.19.1](https://github.com/arandu-io/hesape/compare/v0.19.0...v0.19.1) - 2026-08-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.19.0...v0.19.1

## [v0.19.0](https://github.com/arandu-io/hesape/compare/v0.18.0...v0.19.0) - 2026-08-29

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.18.0...v0.19.0

## [v0.18.0](https://github.com/arandu-io/hesape/compare/v0.17.0...v0.18.0) - 2026-08-28

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.17.0...v0.18.0

## [v0.17.0](https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.7.0...v0.17.0) - 2026-08-27

**Full Changelog**: https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.7.0...v0.17.0

## [v0.16.0](https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.6.2...v0.16.0) - 2026-08-27

**Full Changelog**: https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.6.2...v0.16.0

## [v0.15.2](https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.6.1...v0.15.2) - 2026-08-27

**Full Changelog**: https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.6.1...v0.15.2

## [v0.15.1](https://github.com/arandu-io/hesape/compare/filesystem/s3/v0.6.0...v0.15.1) - 2026-08-27

**Full Changelog**: https://github.com/arandu-io/hesape/compare/filesystem/s3/v0.6.0...v0.15.1

## [v0.15.0](https://github.com/arandu-io/hesape/compare/v0.14.2...v0.15.0) - 2026-08-26

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.14.2...v0.15.0

## [v0.14.2](https://github.com/arandu-io/hesape/compare/v0.14.1...v0.14.2) - 2026-08-24

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.14.1...v0.14.2

## [v0.14.1](https://github.com/arandu-io/hesape/compare/v0.14.0...v0.14.1) - 2026-08-24

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.14.0...v0.14.1

## [v0.14.0](https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.5.0...v0.14.0) - 2026-08-23

**Full Changelog**: https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.5.0...v0.14.0

## [v0.14.2](https://github.com/arandu-io/hesape/compare/v0.14.1...v0.14.2) - 2026-08-24

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.14.1...v0.14.2

## [v0.14.1](https://github.com/arandu-io/hesape/compare/v0.14.0...v0.14.1) - 2026-08-24

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.14.0...v0.14.1

**Full Changelog**: https://github.com/arandu-io/hesape/compare/database/connectors/mysql/v0.5.0...v0.13.0

## [v0.12.0](https://github.com/arandu-io/hesape/compare/v0.11.0...v0.12.0) - 2026-08-21

**Full Changelog**: https://github.com/arandu-io/hesape/compare/v0.11.0...v0.12.0

Written by the release, not by hand. `.github/workflows/release.yml` publishes
the release a tag names and prepends what it said, so the tag, the release and
this file cannot disagree.

Tags cut before that workflow existed have no release and are not listed here.
`git tag` is where they are.
