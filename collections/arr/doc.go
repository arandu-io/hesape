// Package arr mirrors Illuminate\Support\Arr.
//
// The file it answers to is Arr.php, which ships in the Collections component
// and not in Support -- the clone at laravel_illuminate/collections/Arr.php is
// the source used here. The current Laravel at
// reference_laravel/framework/src/Illuminate/Collections/Arr.php declares the
// same fifty-nine statics; the only behavioural difference is a $depth
// argument on dot(), which the pinned clone does not have and which is
// therefore absent here too.
//
// helpers.php contributes data_get, data_set, data_fill, data_forget and
// data_has. They live here rather than in the parent package because each one
// is Arr::get, Arr::set or Arr::forget with wildcard segments added, and they
// need the same accessor primitives.
//
// # Why this is a subpackage
//
// Arr::get, Arr::first, Arr::last, Arr::map, Arr::filter, Arr::only,
// Arr::except, Arr::sort, Arr::where, Arr::random, Arr::take, Arr::wrap,
// Arr::pluck and a dozen more carry the same names as methods and functions of
// Illuminate\Support\Collection. In PHP the two live in different classes, so
// the names never meet. Go has one namespace per package, so they would have
// to be renamed to sit together -- and ADR 0044 says the name is the
// Illuminate name. A subpackage keeps both: collections.Map is Collection::map
// and arr.Map is Arr::map, spelled exactly as Illuminate spells them.
//
// # What a PHP array is here
//
// A PHP array is an ordered map, and Illuminate uses it as two different
// things. The associative array is a map[string]any and the list is a []any --
// or, wherever the element type is not forced to be mixed, a []T.
//
// A function whose PHP result depends on key order has to pick an order that a
// Go map cannot supply. Every one of them -- Divide, Query, Flatten over a
// map, ToCssClasses -- walks the keys in ascending order, and says so. PHP
// walks insertion order there, which is not reproducible from the value alone.
//
// # Defaults and exceptions
//
// Illuminate takes a $default argument wherever a key may be missing. Here the
// second result is false instead, as it is on Collection: the caller writes the
// fallback at the call site. Where the PHP throws, the Go returns an error.
package arr
