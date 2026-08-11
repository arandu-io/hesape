// Package events mirrors Illuminate\Log\Context\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/log/Context/Events:
//
//	ContextDehydrating.php
//	ContextHydrated.php
//
// The clone is the source; it was checked against
// reference_laravel/framework/src/Illuminate/Log/Context/Events and the two are
// identical.
//
// Both events carry the repository, and both carry it as the Repository
// interface declared here rather than as the concrete
// log/context.Repository -- the concrete one dispatches them, and naming it
// would close an import loop. The type doc says so on the interface itself.
package events
