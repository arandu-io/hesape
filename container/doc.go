// Package container is Arandu's answer to Illuminate\Container, and the answer
// is that there is no container.
//
// This package exports nothing and never will. ADR 0001 rejected the dependency
// injection container and ADR 0002 rejected the facades built on top of it. The
// directory exists because a refusal that does not say what to write instead is
// how a Laravel developer ends up searching pkg.go.dev for a container. This
// file is what to write instead.
//
// # Source
//
// Read against the clone at laravel_illuminate/container: Container.php,
// BoundMethod.php, ContextualBindingBuilder.php, RewindableGenerator.php,
// Util.php and the sixteen files under Attributes/. Checked against
// reference_laravel 13.24.0, which adds two attributes the clone does not
// have -- BindWhen and RequestAttribute -- both covered in the attribute table
// below. 64 public methods, all of them accounted for here.
//
// # Why not
//
// A container resolves a type name into a value at runtime, by reflection. In
// PHP that is close to free, because everything is already dynamic and the type
// system is not doing the work anyway. In Go it costs twice.
//
// It costs at runtime, which is the smaller half: reflect.New and a map lookup
// per dependency, on every request, for a graph that never changes after boot.
//
// It costs where it matters at the moment the graph is wrong. `make(Foo)` with
// no binding for Foo is a panic on the first request that touches that path,
// found by a customer. The same mistake in a constructor call is a build
// failure, found by the person who made it, before the commit. Go was chosen
// for that trade; a container spends it.
//
// There is a third cost that is easy to miss. A container is global mutable
// state, and Go tests run in parallel. Laravel gets away with a shared
// container because a PHP process serves one request; the moment it does not --
// Octane -- forgetScopedInstances appears, and with it the class of bug where
// one request sees another request's state. Nothing in this framework has that
// class of bug, because nothing is shared unless a line of code shares it.
//
// # The short answer
//
// A struct with the fields, a New function that takes them, and the assembly
// written out in bootstrap/app.go.
//
//	type ProfileController struct {
//		users UserRepository
//		log   *slog.Logger
//	}
//
//	func NewProfileController(users UserRepository, log *slog.Logger) *ProfileController {
//		return &ProfileController{users: users, log: log}
//	}
//
//	// bootstrap/app.go
//	users := NewSQLUsers(db)
//	profile := NewProfileController(users, log)
//
// The interface is declared by the consumer, not by the provider, so
// *SQLUsers satisfying UserRepository is checked at the call site, at compile
// time, with no registration anywhere.
//
// That answer covers most of Illuminate\Container. It does not cover singleton,
// scoped, tagged and contextual binding, which is why those four have sections
// of their own further down.
//
// # Every method, and what replaces it
//
// Resolution -- make, makeWith, resolve, get, build, factory, call, wrap:
//
//	make / resolve / get   the field is already there: h.users
//	makeWith               function arguments, one per parameter
//	build                  the New function
//	factory                a func value: type ReportFactory func(ctx) (*Report, error)
//	call / wrap            call the method; what does not vary is a field
//
// `call` deserves a word, because BoundMethod.php is where a third of the
// reflection lives. It injects a method's dependencies from its signature.
// In Go the dependencies a method needs and does not receive are fields, put
// there by the constructor, and the ones that vary per call are arguments:
//
//	func (s *SendInvoice) Handle(ctx context.Context, userID string) error {
//		u, err := s.users.Find(ctx, userID)
//		...
//	}
//
// Registration -- bind, bindIf, singleton, singletonIf, scoped, scopedIf,
// instance, alias, extend:
//
//	bind        pass the value to the constructor
//	bindIf      see below
//	singleton   see "singleton"
//	scoped      see "scoped"
//	instance    you already hold the value; hand it over
//	alias       the consumer declares the interface it needs
//	extend      a decorator type implementing the same interface
//
// bindIf, singletonIf and scopedIf ask "is this already registered?", which is
// a question about a runtime map. There is no map. Either the line is in
// bootstrap/app.go or it is not, and you can see which. Where a collaborator is
// genuinely optional, the default belongs in the constructor:
//
//	func NewScheduler(clock Clock) *Scheduler {
//		if clock == nil {
//			clock = systemClock{}
//		}
//		return &Scheduler{clock: clock}
//	}
//
// alias exists in Laravel so that 'mailer' and Mailer::class reach the same
// binding. Here the consumer names what it needs, in its own package, and the
// same concrete type can satisfy three unrelated interfaces without either side
// declaring it.
//
// extend wraps a resolved value to decorate it. A decorator is a type:
//
//	type LoggingUsers struct {
//		next UserRepository
//		log  *slog.Logger
//	}
//
//	func (l LoggingUsers) Find(ctx context.Context, id string) (User, error) {
//		started := time.Now()
//		u, err := l.next.Find(ctx, id)
//		l.log.InfoContext(ctx, "users.find", "id", id, "took", time.Since(started), "err", err)
//		return u, err
//	}
//
//	// bootstrap/app.go
//	users := NewLoggingUsers(NewSQLUsers(db), log)
//
// The Laravel version is invisible from the call site. This one is one line
// with the word Logging in it.
//
// Inspection -- bound, has, resolved, isShared, isAlias, getBindings, getAlias,
// currentlyResolving:
//
//	bound / has             the compiler: App has the field or it does not build
//	resolved                the value exists; there is no lazy slot to interrogate
//	isShared / isAlias      no lifetimes and no aliases to interrogate
//	getBindings             bootstrap/app.go, read top to bottom
//	currentlyResolving      the stack trace, which already says it better
//
// At the one edge where a collaborator is truly optional, `bound` is a nil check
// on a named field -- `a.Billing != nil` -- and the field name says which one.
//
// Lifecycle -- forgetInstance, forgetInstances, forgetScopedInstances, flush,
// dropStaleInstances, forgetExtenders:
//
//	all six       build a new App
//
// These exist to undo a shared container between tests. A test that needs a
// clean application calls the same Build the binary calls, gets its own values,
// and runs in parallel with every other test, because there is nothing shared
// to forget. This is the single largest thing gained by not having a container,
// and it is worth more than everything on this page costs.
//
// Hooks -- beforeResolving, resolving, afterResolving, afterResolvingAttribute,
// rebinding, refresh, bindMethod, hasMethodBinding, callMethodBinding,
// whenHasAttribute:
//
//	resolving family    do it once, in Build, in front of you
//	rebinding/refresh   see below
//	bindMethod family   a func field, or a method on the type
//
// The resolving callbacks run code on every resolution -- configure the logger,
// push a handler, decorate. If it should happen to every instance and there is
// one instance, it happens once, in Build:
//
//	logger := slog.Default().With("env", cfg.App.Env, "component", "billing")
//
// rebinding and refresh fire when a binding is replaced after something already
// resolved it, so the old holder can be updated. Replacing a collaborator on a
// running application is not a thing this framework does. When the underlying
// need is real -- a value reloaded while the process runs and read
// concurrently -- it is one type, and it is honest about the concurrency:
//
//	type Reloadable[T any] struct{ v atomic.Pointer[T] }
//
//	func (r *Reloadable[T]) Load() *T     { return r.v.Load() }
//	func (r *Reloadable[T]) Store(next *T) { r.v.Store(next) }
//
// Everything else -- when, whenHasAttribute, addContextualBinding, needs, give,
// giveTagged, giveConfig, tag, tagged, getInstance, setInstance,
// resolveEnvironmentUsing, currentEnvironmentIs, resolveFromAttribute, and the
// ArrayAccess and __get/__set pairs -- is covered in the four sections below,
// in the attribute table, or is PHP language interface with no Go equivalent.
// getInstance and setInstance are the global container itself; they are what
// ADR 0001 rejected, not a method within it.
//
// # singleton
//
// `$this->app->singleton(Mailer::class, ...)` means: build it once, and hand
// everyone the same one. In Go that is what a variable is.
//
//	func Build(cfg Config, db *sql.DB) App {
//		users := NewSQLUsers(db)
//
//		return App{
//			Profile: NewProfileController(users, log),
//			Billing: NewBillingController(users),
//			Cleanup: NewCleanupTask(users),
//		}
//	}
//
// All three hold the same *SQLUsers. Nothing declares that; it is what passing
// a pointer means. There is no lifetime flag because there is no second
// lifetime: a value built once and shared is shared, and a value built twice
// is two values, and both are visible in the four lines above.
//
// The Laravel version carries a warning this one does not need. Because the
// container decides, `bind` and `singleton` produce code that reads identically
// at the call site, and getting it wrong -- a stateful service registered with
// bind, so each injection gets a fresh one, or a request-specific value
// registered with singleton, so it leaks across requests -- is invisible until
// production. Here the mistake has to be typed: two calls to NewSQLUsers, on
// two lines, next to each other.
//
// What a singleton does bring in Go, and PHP does not have to think about, is
// that a shared value is touched by many goroutines at once. A shared value
// with mutable state needs a mutex or an atomic, and `go test -race` is what
// says so. That is a real obligation, and it is the reason the shared thing is
// usually a handle to something already concurrency-safe -- *sql.DB,
// *slog.Logger, *http.Client -- rather than a bag of fields.
//
// # scoped
//
// This is the one that has no one-line answer, and the one that decides whether
// somebody accepts the absence of a container.
//
// `$this->app->scoped(...)` is one instance per request: built the first time
// it is asked for within a request, shared for the rest of that request,
// discarded at the end. In Go there are two forms, and picking between them is
// the only decision.
//
// Form A: a value on the request's context.Context.
//
//	type grantKey struct{}
//
//	func WithGrant(ctx context.Context, g Grant) context.Context {
//		return context.WithValue(ctx, grantKey{}, g)
//	}
//
//	func GrantFrom(ctx context.Context) (Grant, error) {
//		g, ok := ctx.Value(grantKey{}).(Grant)
//		if !ok {
//			return Grant{}, ErrNoGrant
//		}
//		return g, nil
//	}
//
//	func Authenticate(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			g := grantFor(r)
//			next.ServeHTTP(w, r.WithContext(WithGrant(r.Context(), g)))
//		})
//	}
//
// The key type is unexported and is a struct, not a string. That is not style:
// it means no other package can write that slot and no two packages can collide
// on it, which is the property that makes this safe where a global map is not.
// The accessor returning an error rather than a zero value is the same
// discipline -- an absent Grant has to be handled, not defaulted (RULE 14).
//
// Form B: a field on a value the handler builds and owns.
//
//	type RequestScope struct {
//		Tx    *sql.Tx
//		Start time.Time
//		Log   *slog.Logger
//	}
//
//	func (c *OrderController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
//		tx, err := c.db.BeginTx(r.Context(), nil)
//		if err != nil {
//			http.Error(w, "database unavailable", http.StatusInternalServerError)
//			return
//		}
//		defer tx.Rollback()
//
//		scope := &RequestScope{Tx: tx, Start: time.Now(), Log: c.log.With("path", r.URL.Path)}
//
//		if err := c.place(r.Context(), scope); err != nil {
//			http.Error(w, "could not place order", http.StatusInternalServerError)
//			return
//		}
//		if err := tx.Commit(); err != nil {
//			http.Error(w, "could not place order", http.StatusInternalServerError)
//			return
//		}
//		w.WriteHeader(http.StatusCreated)
//	}
//
// Which one:
//
//   - Form B when the value has a named consumer in a signature you control.
//     The transaction, the per-request loader cache, the accumulated
//     validation errors: everything that touches them is your code, and passing
//     them as an argument means the compiler checks that they arrived. Prefer
//     this. It is the default, and it costs a parameter.
//
//   - Form A when the value must cross code that has no reason to know about
//     it. The Grant is set by middleware, travels through the router, the
//     kernel and the pipeline -- none of which is yours and none of which
//     should grow a parameter for it -- and is read at the bottom by a
//     repository. Deadlines and cancellation travel the same way, which is why
//     context.Context exists at all.
//
// The cost of Form A, stated plainly: the type check is gone. GrantFrom does a
// type assertion at runtime, and a handler that was mounted without the
// middleware compiles and fails at the first request. That is exactly the
// failure mode this package refuses everywhere else, so it is spent
// deliberately, on the few values that genuinely cross the framework, and it is
// spent behind an accessor that returns an error rather than a zero value.
//
// The thing not to do is the third form: a package-level map keyed by request
// ID. That is a container with worse ergonomics, and it leaks whenever a
// handler returns early.
//
// One property comes free and is worth naming. In Laravel, scoped instances
// live on a container that outlives the request, so under Octane they have to
// be forgotten between requests, and forgetting to forget is a data leak
// between users. Here a request-scoped value is a local variable in a handler
// goroutine. When the handler returns, it is garbage. There is nothing to
// forget, and forgetScopedInstances has no counterpart because it has no
// problem to solve.
//
// # tagged
//
// `$this->app->tag([A::class, B::class], 'reports')` then
// `$this->app->tagged('reports')` collects a set of implementations and hands
// them over as a group. The Go answer is a slice, and the tag is the parameter
// name:
//
//	type Notifier struct{ channels []Channel }
//
//	func NewNotifier(channels ...Channel) *Notifier {
//		return &Notifier{channels: channels}
//	}
//
//	// bootstrap/app.go
//	notifier := NewNotifier(MailChannel{}, SMSChannel{})
//
// The three things Laravel's version does that this does not: the string, the
// order being whatever registration order happened to be, and the laziness.
//
// The string is the point of the trade. `tagged('reports')` with a typo returns
// an empty iterable and the feature quietly does nothing; there is no error,
// because an empty tag is legal. The slice cannot be misspelled, and an element
// of the wrong type is a build failure.
//
// The order is now the order in the argument list, which is where somebody
// looking for it will look.
//
// The laziness is RewindableGenerator.php: `tagged` returns a generator so the
// members are only constructed if iterated, and Countable so counting does not
// construct them. That matters when a tag holds twenty services and a request
// uses two. It is a real feature and this drops it. If a member is expensive
// enough to matter, the element type is a factory -- `[]func() Channel` -- and
// the laziness is visible in the type rather than hidden in the container. In
// practice the members are handles, and building them all at boot costs
// nothing and fails at boot instead of on the twentieth request.
//
// giveTagged and giveConfig are the contextual forms of the same thing: a slice
// argument and a config field, respectively.
//
// # when / needs / give
//
// Contextual binding is where the container is at its most convincing in PHP
// and least necessary in Go:
//
//	$this->app->when(PhotoController::class)
//	          ->needs(Filesystem::class)
//	          ->give(fn () => Storage::disk('local'));
//
// It exists because the container resolves by type, so two consumers that need
// the same interface would get the same implementation, and this is the escape
// hatch. Go does not resolve by type. Two consumers get what you pass them:
//
//	photos   := NewPhotoController(LocalDisk{Root: "storage/app"})
//	invoices := NewInvoiceController(S3Disk{Bucket: "invoices"})
//
// That is the whole feature, and it is two lines with no builder, no fluent
// chain and no third concept. `when` is the argument. `needs` is the parameter
// type. `give` is the value.
//
// Nested contextual binding -- give a whole subtree a different implementation
// -- is a constructor argument threaded down, which is more typing than the
// PHP and is also the only way to find out later which subtree got what.
//
// whenHasAttribute, addContextualBinding, resolveFromAttribute and the
// ContextualAttribute interface are the machinery under the attributes, covered
// next.
//
// # Attributes
//
// The sixteen files under Attributes/ are contextual bindings written on a
// constructor parameter. Every one of them is a struct field, filled in
// bootstrap/app.go, or a read from the request:
//
//	#[Auth('web')]              a field: auth *auth.Service, guard chosen at wiring
//	#[Authenticated]            GrantFrom(ctx) -- the Grant carries the caller
//	#[CurrentUser]              GrantFrom(ctx); it subclasses Authenticated
//	#[Cache('redis')]           a field: cache *cache.Repository
//	#[Config('app.name')]       a field on the typed config struct: cfg.App.Name
//	#[Context('trace')]         context.WithValue with an unexported key type
//	#[Database('reporting')]    a field: db *data.DB
//	#[DB]                       same; it subclasses Database
//	#[Give(Foo::class)]         the argument you pass
//	#[Log('billing')]           a field: log *slog.Logger, or log.With("component", ...)
//	#[RouteParameter('id')]     r.PathValue("id") in the handler
//	#[RequestAttribute('x')]    a value on r.Context(), same as #[Context]
//	#[Storage('s3')]            a field: disk *filesystem.Disk
//	#[Tag('reports')]           a slice field; see "tagged"
//	#[Singleton] / #[Scoped]    see "singleton" and "scoped"
//	#[Bind] / #[BindWhen]       a line in bootstrap/app.go; the condition is an if
//
// #[Authenticated] and #[CurrentUser] are worth separating from the rest. They
// resolve the authenticated user from the container, which means any object
// anywhere can ask who is logged in. Here the caller arrives as a
// security.Grant on the request context and reaches the data layer as an
// argument, because RULE 14 makes the Grant the only source of tenant for SQL
// and RULE 17 requires it on reads as well as writes. Making that reachable
// without passing it is precisely what this framework will not do.
//
// #[Config] is the one where the replacement is better rather than merely
// equivalent. `#[Config('app.nmae')]` injects null and the failure surfaces
// somewhere else entirely; cfg.App.Nmae does not build.
//
// #[Bind] and #[BindWhen] put the binding on the class being bound, with
// BindWhen taking a closure and Bind taking a list of environments. A binding
// that varies by environment is an if in Build, next to the other one, where
// both branches are readable at once.
//
// # What is actually given up
//
// Three things, and pretending otherwise would make the rest of this document
// less trustworthy.
//
// Verbosity. Build in bootstrap/app.go is long and grows with the application.
// It is the price of the file being the answer to "where does this come from",
// and `aru make:module` prints the lines to paste rather than editing it, so
// the file is never something a generator wrote (ADR 0001, ADR 0023).
//
// Laziness. The container builds a service the first time it is asked for; Build
// builds everything at boot. For a handle -- a pool, a client, a logger -- that
// is free and it fails at boot rather than at the first request that needs it,
// which is better. For something genuinely expensive and rarely used, the
// answer is a func field, and the deferral is then a thing in the type rather
// than a property of the framework.
//
// Runtime substitution. A package cannot reach into a running application and
// replace a binding. Tests do it by building the application with a different
// argument, which is the same substitution done at the only moment it is safe.
//
// # Nothing is exported here
//
// This package deliberately has no API. If a symbol appears in it, something
// has gone wrong: ADR 0045 is the document that says what to write instead, and
// this file is its doc-comment form.
package container
