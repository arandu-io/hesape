package collections

import (
	"cmp"
	"slices"
)

// This file holds the Illuminate methods that only mean something when the
// keys of the collection are keys and not positions: everything the PHP
// implements with array_diff_assoc, array_intersect_key, ksort and their kin.
//
// A Collection[T] is a list, so those take a map[K]V instead. The PHP array
// they answer to is an ordered map; a Go map has no order, so a method whose
// PHP result depends on key order returns a Collection[V], which does.

// sortedKeys returns the keys of m in ascending order. Go randomises map
// iteration on purpose, and every function here that produces a list has to be
// reproducible, so nothing in this file walks a map without going through it.
func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// DiffAssoc is Collection::diffAssoc, which the PHP implements with
// array_diff_assoc: the entries whose key and value together are absent from
// items.
//
// An entry survives when items has no such key, or has it with another value.
// The result is empty rather than nil.
func DiffAssoc[K comparable, V comparable](array, items map[K]V) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		if other, ok := items[k]; ok && other == v {
			continue
		}
		out[k] = v
	}
	return out
}

// DiffAssocUsing is Collection::diffAssocUsing, which the PHP implements with
// array_diff_uassoc: DiffAssoc with the keys compared by the callback rather
// than by equality.
//
// As in the PHP the callback compares keys only; values are still compared by
// equality. It reports zero when two keys are the same key.
func DiffAssocUsing[K comparable, V comparable](array, items map[K]V, compare func(a, b K) int) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		matched := false
		for otherKey, otherValue := range items {
			if compare(k, otherKey) == 0 && otherValue == v {
				matched = true
				break
			}
		}
		if !matched {
			out[k] = v
		}
	}
	return out
}

// DiffKeys is Collection::diffKeys, which the PHP implements with
// array_diff_key: the entries whose key is absent from items, whatever the
// values on either side are.
//
// items may hold a different value type for exactly that reason: the PHP types
// its parameter iterable<TKey, mixed>.
func DiffKeys[K comparable, V, W any](array map[K]V, items map[K]W) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		if _, ok := items[k]; ok {
			continue
		}
		out[k] = v
	}
	return out
}

// DiffKeysUsing is Collection::diffKeysUsing, which the PHP implements with
// array_diff_ukey: DiffKeys with the keys compared by the callback.
func DiffKeysUsing[K comparable, V, W any](array map[K]V, items map[K]W, compare func(a, b K) int) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		matched := false
		for otherKey := range items {
			if compare(k, otherKey) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			out[k] = v
		}
	}
	return out
}

// IntersectAssoc is Collection::intersectAssoc, which the PHP implements with
// array_intersect_assoc: the entries items has under the same key with the same
// value.
func IntersectAssoc[K comparable, V comparable](array, items map[K]V) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		if other, ok := items[k]; ok && other == v {
			out[k] = v
		}
	}
	return out
}

// IntersectAssocUsing is Collection::intersectAssocUsing, which the PHP
// implements with array_intersect_uassoc: IntersectAssoc with the keys compared
// by the callback.
func IntersectAssocUsing[K comparable, V comparable](array, items map[K]V, compare func(a, b K) int) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		for otherKey, otherValue := range items {
			if compare(k, otherKey) == 0 && otherValue == v {
				out[k] = v
				break
			}
		}
	}
	return out
}

// IntersectByKeys is Collection::intersectByKeys, which the PHP implements with
// array_intersect_key: the entries whose key items also has.
//
// The values kept are this map's, never items'.
func IntersectByKeys[K comparable, V, W any](array map[K]V, items map[K]W) map[K]V {
	out := make(map[K]V, len(array))
	for k, v := range array {
		if _, ok := items[k]; ok {
			out[k] = v
		}
	}
	return out
}

// SortKeys is Collection::sortKeys, which the PHP implements with ksort.
//
// The PHP returns a collection still carrying its keys, in key order. A Go map
// cannot carry an order, so the ordering is the result: the values, ascending
// by key. Keys over the same map gives the keys in the matching order.
func SortKeys[K cmp.Ordered, V any](array map[K]V) Collection[V] {
	keys := sortedKeys(array)
	out := make(Collection[V], len(keys))
	for i, k := range keys {
		out[i] = array[k]
	}
	return out
}

// SortKeysDesc is Collection::sortKeysDesc, which the PHP implements with
// krsort: SortKeys with the order reversed.
func SortKeysDesc[K cmp.Ordered, V any](array map[K]V) Collection[V] {
	return SortKeys(array).Reverse()
}

// SortKeysUsing is Collection::sortKeysUsing, which the PHP implements with
// uksort: the values ordered by the callback applied to their keys.
//
// The keys are put in ascending order before the callback sorts them, so that
// keys the callback calls equal come out in a fixed order instead of the random
// one Go gives map iteration. The PHP inherits the array's insertion order
// there, which is just as arbitrary and rather less reproducible.
func SortKeysUsing[K cmp.Ordered, V any](array map[K]V, compare func(a, b K) int) Collection[V] {
	keys := sortedKeys(array)
	stableSort(keys, compare)
	out := make(Collection[V], len(keys))
	for i, k := range keys {
		out[i] = array[k]
	}
	return out
}

// Keys is Collection::keys for a collection whose keys are keys, where the
// method of the same name on Collection[T] answers for a list.
//
// They come out ascending, which is the order SortKeys puts their values in, so
// the two walked together pair each value with the key it came from.
func Keys[K cmp.Ordered, V any](array map[K]V) Collection[K] {
	return Collection[K](sortedKeys(array))
}

// MergeRecursive is Collection::mergeRecursive, which the PHP implements with
// array_merge_recursive.
//
// Where both sides hold a map under a key, the maps merge. Where a key is on
// both sides and either side is not a map, the two values become a []any
// holding both -- a value already a []any contributes its elements rather than
// itself, which is how array_merge_recursive grows a list across repeated
// merges. Merge, without the recursion, keeps only the second value.
func MergeRecursive(array, items map[string]any) map[string]any {
	out := make(map[string]any, len(array)+len(items))
	for k, v := range array {
		out[k] = v
	}
	for k, v := range items {
		existing, ok := out[k]
		if !ok {
			out[k] = v
			continue
		}
		leftMap, leftIsMap := existing.(map[string]any)
		rightMap, rightIsMap := v.(map[string]any)
		if leftIsMap && rightIsMap {
			out[k] = MergeRecursive(leftMap, rightMap)
			continue
		}
		out[k] = append(asList(existing), asList(v)...)
	}
	return out
}

// asList is PHP's promotion of a scalar to an array before array_merge_recursive
// concatenates the two.
func asList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}

// ReplaceRecursive is Collection::replaceRecursive, which the PHP implements
// with array_replace_recursive.
//
// items wins at every key, except where both sides hold a map or both hold a
// []any: those descend, so replacing {"a":[1,2,3]} with {"a":[9]} gives
// {"a":[9,2,3]} rather than {"a":[9]}. Replace, without the recursion, gives
// the latter.
func ReplaceRecursive(array, items map[string]any) map[string]any {
	out := make(map[string]any, len(array)+len(items))
	for k, v := range array {
		out[k] = v
	}
	for k, v := range items {
		existing, ok := out[k]
		if !ok {
			out[k] = v
			continue
		}
		out[k] = replaceRecursiveValue(existing, v)
	}
	return out
}

func replaceRecursiveValue(existing, replacement any) any {
	if leftMap, ok := existing.(map[string]any); ok {
		if rightMap, ok := replacement.(map[string]any); ok {
			return ReplaceRecursive(leftMap, rightMap)
		}
		return replacement
	}
	leftList, leftIsList := existing.([]any)
	rightList, rightIsList := replacement.([]any)
	if !leftIsList || !rightIsList {
		return replacement
	}
	out := make([]any, len(leftList))
	copy(out, leftList)
	for i, v := range rightList {
		if i < len(out) {
			out[i] = replaceRecursiveValue(out[i], v)
			continue
		}
		out = append(out, v)
	}
	return out
}

// CollapseWithKeys is Collection::collapseWithKeys: the maps in the collection
// flattened into one, keeping their keys.
//
// The PHP folds them with array_replace, so a key held by more than one element
// ends up with the value of the last. Elements that are not arrays are dropped
// there; in Go the element type says they cannot occur.
func CollapseWithKeys[K comparable, V any](c Collection[map[K]V]) map[K]V {
	out := make(map[K]V)
	for _, item := range c {
		for k, v := range item {
			out[k] = v
		}
	}
	return out
}
