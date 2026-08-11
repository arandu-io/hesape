// Package concerns mirrors Illuminate\Database\Eloquent\Relations\Concerns: the
// traits the sixteen relation types are assembled from.
//
// It answers, from the clone at laravel_illuminate/database/Eloquent/Relations/
// Concerns:
//
//	AsPivot.php                    -> aspivot.go
//	CanBeOneOfMany.php             -> canbeoneofmany.go
//	ComparesRelatedModels.php      -> comparesrelatedmodels.go
//	InteractsWithDictionary.php    -> interactswithdictionary.go
//	InteractsWithPivotTable.php    -> interactswithpivottable.go
//	SupportsDefaultModels.php      -> supportsdefaultmodels.go
//	SupportsInverseRelations.php   -> supportsinverserelations.go
//
// # A trait is a struct you embed, and its abstract methods are fields
//
// A PHP trait is compiled into the class that uses it, so it reads $this->query
// and calls abstract methods the class supplies. Go composes instead: the trait
// becomes a struct the relation embeds, which gives it the same fields and the
// same promoted methods, and each abstract method the PHP declares arrives as a
// function field the relation sets when it builds itself. That is the shape
// throughout this package -- ComparesRelatedModels.ParentKey,
// SupportsDefaultModels.NewRelatedInstanceFor, CanBeOneOfMany.AddConstraints --
// and it is mechanical, not a redesign.
//
// # Why Model and Builder are declared here
//
// Model and Builder are the surfaces a relation asks of
// Illuminate\Database\Eloquent\Model and \Builder. They are declared in this
// leaf package rather than in relations because Go forbids a subpackage from
// importing its parent, and both packages have to agree on one type. relations
// aliases them straight back, so relations.Model and concerns.Model are the
// same type and the PHP layout still reads.
//
// # Everything that runs takes the Grant
//
// Attach, Detach, Sync, Toggle, UpdateExistingPivot and every read behind them
// take a context and an auth.Grant, and every statement they build is filtered
// by the tenant that Grant carries -- including the INSERT, which stamps it.
// RULE 17 does not have a write half and a read half, and the pivot table is
// where it is easiest to forget: the query that says which roles a user has is
// two joins away from the policy that authorized the user.
package concerns
