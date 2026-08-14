// Package concerns mirrors Illuminate\View\Concerns.
//
// The files it answers to, in the clone at
// laravel_illuminate/view/Concerns:
//
//	ManagesComponents.php
//	ManagesEvents.php
//	ManagesFragments.php
//	ManagesLayouts.php
//	ManagesLoops.php
//	ManagesStacks.php
//	ManagesTranslations.php
//
// Nothing is implemented here, and nothing will be. A concern is a PHP trait:
// a set of methods mixed into a class from outside it. Go has no such thing --
// a method's receiver must be declared in the same package as the method -- so
// what a trait carries lands directly on the type that had it, in the package
// that declares that type.
//
// It is the same reason ADR 0046 gives for Macroable and Conditionable not
// being packages, and it applies here without a second decision being needed.
package concerns
