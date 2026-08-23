---
name: hesape-grant
description: How a component of the hesape collection reaches stored data — the auth.Grant it takes, the tenant it derives, and the three places that deliberately have neither. Use when adding or changing a method that reads or writes a row, a cache entry, a file, a queued job, a notification or a broadcast; when a signature will not compile and the missing argument is a Grant; when building a key, a path or a prefix that will hold more than one customer's data; when the request mentions "tenant", "multi-tenant", "tenant isolation", "scope this query", "who can see this", "SystemGrant", or "why does this take a Grant"; and when tempted to drop the parameter to make something compile. Covers the signature shape, auth.Tenant and Grant.Check, the key formats, what is not scoped by tenant and the file that argues each, and the test that proves the rule by compiling code that must fail.
license: MIT
---

# The Grant, inside a component

`auth.Grant` has only unexported fields, so nothing outside the `auth` package
can write one as a struct literal. Every method in this collection that reaches
stored data takes one, before the identifier:

```go
func (r *Repository) Find(ctx context.Context, g auth.Grant, id string) (*T, error) {
	if err := g.Check(actionView); err != nil {
		return nil, err
	}
	tenant := auth.Tenant(g)   // never from a path, a body, a query or a header
	...
}
```

That is the thesis of the framework, and it is proven by compiling rather than
asserted: `TestRepositoryWithoutGrantDoesNotCompile`, in
`database/grant_required_test.go:18`, runs the Go toolchain over two fixtures
under `database/testdata/` and requires each to fail with a specific message —
`not enough arguments in call to repo.Find` for the call with no Grant, and
`cannot refer to unexported field valid` for the struct literal. A fixture that
failed for an unrelated reason would prove nothing, so the message is checked
too.

Those two fixtures are also why `testdata/` is excluded from the `gofmt` gate.
They are invalid on purpose.

## The procedure

**1. Take the Grant as the parameter after the context.** Not last, not
optional, not a field on the receiver. A receiver that holds one is a receiver
that outlives the request it was authorized for.

**2. Call `Check` with the action this method performs.** `Grant.Check` fails
the zero value, fails a `SystemGrant` that was refused, and fails a Grant issued
for a different action — which is what catches copy-paste between repository
methods.

**3. Derive the tenant with `auth.Tenant(g)`, and refuse an empty one.** Every
component does this the same way, and each has a sentinel for it:

```go
cache.ErrNoTenant       // cache/store.go:136
filesystem.ErrNoTenant  // filesystem/key.go:14
```

An empty tenant is a bug in the caller, not a miss. `cache.Remember` says so
outright: anything other than a missing tenant falls through and computes, and a
missing tenant returns the error, because computing anyway would hide it until
the day the value is wrong.

**4. Validate it before it becomes a key.** `auth.ValidTenant` is what turns a
tenant into a namespace. Skipping it was a real defect in `filesystem`: tenant
`acme/reports` with key `q1.pdf` and tenant `acme` with key `reports/q1.pdf`
resolved to the same object, each holding a valid Grant and violating no Policy,
because the path is built after the Policy runs. The argument is written at
`filesystem/key.go:34-41`.

**5. Build the key with the tenant in it.**

```
cache:<tenant>:<namespace>:<key>      cache/repository.go:988-999
<tenant>/<key>                        filesystem/key.go:19
```

Build it in one place, and hand the result to the adapter. `filesystem.Key` is
the model: an `Adapter` is given the finished path and never sees the Grant, so
there is no driver in which the prefix can be forgotten.

## When there is no subject

`auth.SystemGrant(action, tenant)` is the named escape hatch, for a scheduled
task, a queue worker or a migration runner. It is exported and auditable on
purpose — the system subject carries the `system` role so an audit can find it —
and it **requires a tenant**: `SystemGrant(action, "")` returns the zero Grant,
which fails `Check`. See `TestSystemGrantRequiresATenant`,
`auth/policy_test.go:129`.

What stops a *handler* from reaching for it is a lint, not the type system. The
doc comment on `Grant` says so in as many words, and says why saying it wrong
would be worse there than anywhere else: that comment is where a reader checks
the thesis.

## What is not scoped by tenant, and why

Three things in this collection deliberately have no tenant prefix. Each carries
its reason next to the code, and none of them is an oversight to be fixed.

**The session.** `SessionHandler` (`session/store.go:59`) and the `Cache`
interface it uses (`session/handlers.go:431-438`) are keyed by the session id
alone — `Read(ctx, sessionID)`, `Get(ctx, key)` — with no Grant anywhere in
either signature. The session id is what *tells you*
which tenant: the tenant is a field on the record you get back
(`session/session.go:102-104`), and prefixing the lookup would mean knowing the
answer before asking the question. Where the tenant does become part of the
question it is required: `RecordStore.DestroyOthers` refuses a record with an
empty tenant or an empty subject id, because "every session of subject 1" with
no tenant reaches every customer (`session/session.go:40-51`).

**The rate limit.** `cache.Limit.Key` identifies the caller being limited — an
IP, a session id, an account — and its doc comment states the rest:
`cache/ratelimiter.go:24-26`. Rate limiting runs *before* authentication on the
routes where it matters most, so there is no Grant to take a tenant from. The
`RateLimiter` therefore takes a key rather than a Grant.

**The scheduler lock.** `lockKey` is `"lock:" + name`, and
`cache/lock.go:322-329` argues the case: a scheduler lock covers the whole
instance, and a lock that existed per tenant would let N replicas each run the
task for a different tenant at the same time — which is the problem, not the
solution. A lock that really is about one tenant puts the tenant in its own
name, which is what `Repository.WithoutOverlapping` does.

`cache/doc.go:18-21` states the last two of these as the exceptions they are, in
the package that owns both, and says why the signatures differ: locks and the
`RateLimiter` take a name and a key, and everything else in `cache` takes a
`Grant`.

This is also why there is no static check for "a key with no tenant prefix": it
would fail `cache`'s own rate limiter and lock. For the cache entries where the
prefix is mandatory the signature closes the door instead — the methods take a
Grant, and without a tenant they return `ErrNoTenant`.

## The line not to cross

If a method will not compile because you have no Grant to pass, the answer is
never to remove the parameter. Ask the caller for one, or use `SystemGrant` with
a tenant if there is genuinely no subject. If neither fits, the design is wrong
rather than the compiler, and saying so is the right output.

Reads are not exempt. `List`, `Find`, a read model, a projection, a report and
an export all take a Grant and all filter by `auth.Tenant(g)`. A read path that
skips it is a cross-tenant leak with a technical name.
