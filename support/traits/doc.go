// Package traits mirrors Illuminate\Support\Traits.
//
// The files it answers to, in the clone at
// laravel_illuminate/support/Traits:
//
//	CapsuleManagerTrait.php
//	Dumpable.php
//	ForwardsCalls.php
//	InteractsWithData.php
//	Localizable.php
//	ReflectsClosures.php
//	Tappable.php
//
// A PHP trait is copied into the class that uses it, so its methods answer as
// that class's own. Go has no traits, and a package of them would be a package
// nobody imports on purpose. So the five that are mirrored live in the support
// package, next to the types that use them:
//
//	Dumpable          -> support.Dump, support.Dd
//	ForwardsCalls     -> support.ForwardCallTo, support.ForwardDecoratedCallTo
//	InteractsWithData -> embedded in support.Fluent, support.UriQueryString
//	                     and support.ValidatedInput
//	Localizable       -> support.WithLocale
//	Tappable          -> support.Tap
//
// CapsuleManagerTrait serves the container, which ADR 0001 rejected.
// ReflectsClosures reads a closure's parameter types, which Go cannot do: a
// func value carries no names and no parameter list at run time.
package traits
