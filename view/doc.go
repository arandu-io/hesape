// Package view is the view layer: kyse for markup, HTMX for interaction,
// Alpine for ephemeral client state, Tailwind for style. It is a binary and it
// is never Node.
//
// A project that uses it still runs with `git clone && aru dev`: no
// node_modules, no package.json, no lockfile of JavaScript, nothing installed
// beyond Go and the standalone binaries the CLI fetches. Having a build step is
// allowed; being Node is not (RULE 13).
//
// It lives in the collection and the views do not, and that split is
// deliberate: resources/views/ belongs to the project, because it is edited;
// the rendering machinery belongs here, because it is not. It used to be a
// repository of its own, and dissolving it is ADR 0021.
//
// The error page deliberately does not use this package. It has to render when
// the rest is broken, including when the view build failed, so it stays as
// html/template inline in hesape/exception.
//
// # What it holds
//
// Register and Registered are the registry a generated view writes itself into
// from init(), the same shape as a database/sql driver. Renderer draws one of
// them to a response, and RenderToString draws one for a message nobody is
// waiting on a socket for. Text, Yield, Include and CSRF are what generated
// code calls; everything decidable at build time already was, so what is left
// is small on purpose. Page is the chrome every screen embeds, carrying
// the messages and the typed input of a rejected attempt without any handler
// having to remember them. The assets -- HTMX, Alpine, the stylesheet -- are
// embedded and served content-addressed, because the CSP is script-src 'self'.
//
// # Illuminate
//
// It mirrors Illuminate\View. The files it answers to, in the clone at
// laravel_illuminate/view:
//
//	AnonymousComponent.php
//	AppendableAttributeValue.php
//	Component.php
//	ComponentAttributeBag.php
//	ComponentSlot.php
//	DynamicComponent.php
//	Factory.php
//	FileViewFinder.php
//	InvokableComponentVariable.php
//	View.php
//	ViewException.php
//	ViewFinderInterface.php
//	ViewName.php
//	ViewServiceProvider.php
//
// Two of those have no counterpart here and will not grow one by accident.
// There is no finder: a view is not looked up on disk at request time, it is
// compiled into the binary and registered by name, which is why a missing view
// is a boot-time panic or a named error rather than a blank page. And there is
// no service provider: Module below registers the one route this package owns
// and hands over the renderer, and nothing is resolved out of a container
// (ADR 0001).
//
// # What is not ported, and why
//
// Eleven public methods of the component have no name here. Each one, with the
// ADR 0056 reason number:
//
//	ViewServiceProvider::register, ::registerFactory, ::registerViewFinder,
//	    ::registerBladeCompiler, ::registerEngineResolver, ::registerFileEngine,
//	    ::registerPhpEngine and ::registerBladeEngine -- reason 2, all eight.
//	    They bind 'view', 'view.finder', 'blade.compiler' and
//	    'view.engine.resolver' into the container and register three engines
//	    against the resolver, which is the choice of which template language a
//	    file is written in. There is one (RULE 9), it is kyse, and it is chosen
//	    by the extension at build time rather than by a resolver at request
//	    time, so there is nothing to resolve and nothing to bind. [Module] is
//	    what a kernel registers instead.
//	Factory::getContainer and Factory::setContainer -- reason 2: PHP holds the
//	    container so that a view composer named by class string can be resolved
//	    when the view runs. A composer here is the function itself, passed to
//	    [Factory.Composer], so there is nothing to resolve it with.
//	ViewException::report -- reason 2: its body is
//	    Container::getInstance()->call([$previous, 'report']), which forwards the
//	    report to whatever the wrapped exception does with one. Unwrapping is
//	    errors.As here, and reporting is exception.Handler.Report, which already
//	    walks the wrapped error -- so a wrapper that only forwards the call has
//	    nothing left to do.
//
// The compiler that turns a .kyse.go file into the Go this package runs is not
// in here yet. It is aru/internal/kyse today, and 20-components/DOC-hesape-reorganization.md
// moves it to view/compilers conditioned on ADR 0047.
package view
