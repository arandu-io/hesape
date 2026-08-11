// Package events mirrors Illuminate\Log\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/log/Events:
//
//	MessageLogged.php
//
// The clone is the source; it was checked against
// reference_laravel/framework/src/Illuminate/Log/Events and the two are
// identical, so nothing here comes from the second source.
//
// There is one event, and it carries what Illuminate carries: the level, the
// formatted message and the merged context. The only change is the type of the
// level, which is slog.Level rather than the PSR-3 word, and the field doc says
// so.
//
// The package is separate from log for the reason Illuminate keeps it separate:
// a listener imports the event, and importing the event should not drag in the
// logger.
package events
