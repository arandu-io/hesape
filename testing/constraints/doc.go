// Package constraints mirrors Illuminate\Testing\Constraints.
//
// The files it answers to, in the clone at
// laravel_illuminate/testing/Constraints:
//
//	ArraySubset.php
//	CountInDatabase.php
//	HasInDatabase.php
//	NotSoftDeletedInDatabase.php
//	SeeInOrder.php
//	SoftDeletedInDatabase.php
//
// [ArraySubset] and [SeeInOrder] are here. The four database constraints are
// not: they ask a connection whether a row is there, which is the assertion
// half of hesape/arandutest, where the connection already is.
//
// [Constraint] is PHPUnit\Framework\Constraint\Constraint, which the clone
// extends but does not carry. Its three methods are the ones the clone
// overrides -- matches, failureDescription and toString -- and nothing else,
// because nothing else is reached from here.
package constraints
