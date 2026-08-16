package jsonschema

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxNodes is the number of schema fragments a single [Deserialize] may expand
// before it gives up.
//
// It is the only defence against a document that is small on disk and enormous
// once its references are followed -- a hundred definitions, each referring to
// the previous one ten times, is a few kilobytes that expands past every bound.
// The reference cache does not help, because each expansion is a distinct node
// with its own sibling keys.
const maxNodes = 20000

// FromArray builds a type from a raw map of the supported JSON Schema subset.
// It delegates to [Deserialize].
//
// The map is what [encoding/json.Unmarshal] produces for an any: every number
// is a float64 and every object is a map[string]any. [Serialize] produces the
// same shape, so a document this package rendered reads back through here.
//
// A schema this package cannot represent is an error naming what it could not
// read.
func FromArray(schema map[string]any) (Type, error) {
	return Deserialize(schema)
}

// Deserialize builds a type from the supported JSON Schema subset.
//
// It reads what this package writes, and the part of JSON Schema that maps onto
// it: the six kinds, the type-as-a-list union, local "$ref" pointers, and the
// nullable "anyOf"/"oneOf" shape that a generator emits for an optional value.
// Anything outside that subset is an error naming the keyword, never a silent
// drop -- a schema that deserializes into something weaker than it says is a
// validator that passes values the document forbids.
//
// # What it cannot keep
//
// Property order. A JSON object has none once decoded into a Go map, and
// [ObjectType] renders its properties in declaration order, so the properties
// of a deserialized object are ordered by name. A document round-tripped
// through [Serialize] and back therefore renders with its properties sorted;
// everything else about it is unchanged.
//
// # Objects come back as they were written
//
// [Object] closes an object because that is this package's default (see the
// package comment). This does not: an object is closed only when the document
// says additionalProperties is false, because the job here is to read a schema
// faithfully rather than to tighten one. [ObjectType.WithoutAdditionalProperties]
// closes one that came back open.
func Deserialize(schema map[string]any) (Type, error) {
	d := &deserializer{root: schema, refCache: map[string]map[string]any{}}
	return d.build(schema, nil)
}

// deserializer reads one document. It is unexported because there is one
// instance per document, reached only through [Deserialize].
type deserializer struct {
	// root is the document local "$ref" pointers resolve against.
	root map[string]any
	// nodes are the fragments expanded so far.
	nodes int
	// refCache holds the resolved "$ref" targets.
	refCache map[string]map[string]any
}

// build makes a type out of one schema fragment.
func (d *deserializer) build(schema map[string]any, refs []string) (Type, error) {
	d.nodes++
	if d.nodes > maxNodes {
		return nil, fmt.Errorf("jsonschema: the JSON Schema is too large to deserialize; it expands beyond [%d] fragments", maxNodes)
	}

	schema, refs, err := d.resolveRef(schema, refs)
	if err != nil {
		return nil, err
	}

	schema, nullableFromUnion, refs, err := d.normalizeUnions(schema, refs)
	if err != nil {
		return nil, err
	}

	names, name, nullableFromType, err := d.resolveType(schema)
	if err != nil {
		return nil, err
	}

	var t Type
	switch {
	case names != nil:
		if err := ensureUnionConstraintsAreSupported(schema); err != nil {
			return nil, err
		}
		if t, err = newUnion(names); err != nil {
			return nil, err
		}
	case name == "object":
		t, err = d.buildObject(schema, refs)
	case name == "array":
		t, err = d.buildArray(schema, refs)
	case name == "string":
		t, err = buildString(schema)
	case name == "integer":
		t, err = buildInteger(schema)
	case name == "number":
		t, err = buildNumber(schema)
	case name == "boolean":
		t = Boolean()
	default:
		return nil, fmt.Errorf("jsonschema: unsupported JSON Schema type [%s]", name)
	}
	if err != nil {
		return nil, err
	}

	if err := applyCommon(t, schema); err != nil {
		return nil, err
	}

	if nullableFromUnion || nullableFromType {
		t.meta().nullable = true
	}
	return t, nil
}

// buildObject makes an object out of a schema fragment.
//
// The properties are ordered by name -- see [Deserialize] for why there is no
// other order to give them.
func (d *deserializer) buildObject(schema map[string]any, refs []string) (*ObjectType, error) {
	var properties []Property
	if members, ok := objectEntries(schema["properties"]); ok {
		required := requiredNames(schema["required"])
		for _, member := range members {
			// A boolean schema -- "properties": {"a": true} -- is legal JSON
			// Schema and has no type to build, so it is refused by name.
			definition, ok := member.value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("jsonschema: unable to represent the schema for property [%s]; boolean schemas are not supported", member.key)
			}
			property, err := d.build(definition, refs)
			if err != nil {
				return nil, err
			}
			if required[member.key] {
				property.meta().required = true
			}
			properties = append(properties, Prop(member.key, property))
		}
	}

	t := Object(properties...)
	// [Object] starts closed, because that is this package's default for a
	// schema someone writes. A schema someone reads keeps what it said, so the
	// keyword is cleared unless the document spelled out false.
	if b, ok := schema["additionalProperties"].(bool); !ok || b {
		t.additionalProperties = nil
	}
	return t, nil
}

// buildArray makes a list out of a schema fragment.
func (d *deserializer) buildArray(schema map[string]any, refs []string) (*ArrayType, error) {
	t := Array()

	// "items": {} and "items": [] are both empty and are skipped.
	// A non-empty list is a tuple and a bool is a boolean schema; neither has a
	// single element type, which is the only shape [ArrayType.Items] carries.
	if raw, ok := schema["items"]; ok && raw != nil && !isEmptyContainer(raw) {
		definition, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New(`jsonschema: tuple and boolean JSON Schema "items" are not supported`)
		}
		item, err := d.build(definition, refs)
		if err != nil {
			return nil, err
		}
		t.Items(item)
	}

	if n, ok := intKeyword(schema, "minItems"); ok {
		t.Min(n)
	}
	if n, ok := intKeyword(schema, "maxItems"); ok {
		t.Max(n)
	}
	// A document that says uniqueItems: false leaves the type alone rather
	// than clearing anything.
	if raw, ok := schema["uniqueItems"]; ok && raw != nil && truthy(raw) {
		t.Unique()
	}
	return t, nil
}

// buildString makes a string out of a schema fragment.
func buildString(schema map[string]any) (*StringType, error) {
	t := String()

	if n, ok := intKeyword(schema, "minLength"); ok {
		t.Min(n)
	}
	if n, ok := intKeyword(schema, "maxLength"); ok {
		t.Max(n)
	}
	if raw, ok := schema["pattern"]; ok && raw != nil {
		expr := phpString(raw)
		// [StringType.Pattern] compiles with MustCompile, which is right for a
		// schema written in Go -- a typo there is a mistake in the program. This
		// expression came out of a file, so it is an error instead: finding
		// out at read time is the same answer, earlier.
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("jsonschema: the JSON Schema [pattern] constraint %q is not a regular expression this package can compile: %w", expr, err)
		}
		t.pattern = re
	}
	if raw, ok := schema["format"]; ok && raw != nil {
		t.Format(phpString(raw))
	}
	return t, nil
}

// buildInteger makes a whole number out of a schema fragment. It is
// applyNumericBounds with the bounds narrowed to whole numbers.
func buildInteger(schema map[string]any) (*IntegerType, error) {
	t := Integer()
	err := applyNumericBounds(schema, func(keyword string, v float64) error {
		n, err := toInteger(v)
		if err != nil {
			return err
		}
		switch keyword {
		case "minimum":
			t.Min(n)
		case "maximum":
			t.Max(n)
		case "multipleOf":
			t.MultipleOf(n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// buildNumber makes a number out of a schema fragment. It is
// applyNumericBounds with the bounds left as they are.
func buildNumber(schema map[string]any) (*NumberType, error) {
	t := Number()
	err := applyNumericBounds(schema, func(keyword string, v float64) error {
		switch keyword {
		case "minimum":
			t.Min(v)
		case "maximum":
			t.Max(v)
		case "multipleOf":
			t.MultipleOf(v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// applyNumericBounds reads the three bound keywords and hands each to the
// setter for the type being built.
//
// The setter is a callback rather than a parameter because IntegerType and
// NumberType take different argument types.
func applyNumericBounds(schema map[string]any, set func(keyword string, v float64) error) error {
	for _, keyword := range []string{"minimum", "maximum", "multipleOf"} {
		raw, ok := schema[keyword]
		if !ok || raw == nil {
			continue
		}
		v, ok := toNumber(raw)
		if !ok {
			return fmt.Errorf("jsonschema: the JSON Schema [%s] constraint must be a number", keyword)
		}
		if err := set(keyword, v); err != nil {
			return err
		}
	}
	return nil
}

// applyCommon reads the keywords every kind carries.
func applyCommon(t Type, schema map[string]any) error {
	m := t.meta()

	if raw, ok := schema["title"]; ok && raw != nil {
		m.title = phpString(raw)
	}
	if raw, ok := schema["description"]; ok && raw != nil {
		m.description = phpString(raw)
	}
	if raw, ok := schema["enum"]; ok && raw != nil {
		if values, ok := objectEntries(raw); ok {
			m.enum = make([]any, len(values))
			for i, v := range values {
				m.enum[i] = v.value
			}
		}
	}
	// Presence, not non-nil: a "default": null is present and is refused,
	// because null as a default is indistinguishable from having none and
	// would render as a keyword the type never carried.
	if raw, ok := schema["default"]; ok {
		if raw == nil {
			return errors.New("jsonschema: a null JSON Schema [default] is not supported")
		}
		m.def, m.hasDefault = raw, true
	}
	return nil
}

// resolveType reads the "type" keyword, which is a name or a list of names.
// The list comes back first and is non-nil only when more than one name
// survives.
//
// The bool is whether "null" was among the names, which is the type-as-a-list
// spelling of nullable.
func (d *deserializer) resolveType(schema map[string]any) (names []string, name string, nullable bool, err error) {
	raw, present := schema["type"]

	if list, ok := raw.([]any); ok {
		var kept []string
		seen := map[string]bool{}
		for _, v := range list {
			// Strictly the string "null", as in_array(..., true) and the !==
			// filter both are: a JSON null in the list is not the null type.
			if s, ok := v.(string); ok && s == "null" {
				nullable = true
				continue
			}
			text := phpString(v)
			if seen[text] {
				continue
			}
			seen[text] = true
			kept = append(kept, text)
		}
		if len(kept) > 1 {
			return kept, "", nullable, nil
		}
		if len(kept) == 1 {
			return nil, kept[0], nullable, nil
		}
		// Only "null" was listed, so there is no kind yet and the shape has to
		// say what it is -- which is what an absent "type" does too.
		raw, present = nil, false
	}

	if present && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return nil, "", nullable, errors.New("jsonschema: unable to determine the JSON Schema type for the given schema")
		}
		return nil, text, nullable, nil
	}

	inferred, ok := inferType(schema)
	if !ok {
		return nil, "", nullable, errors.New("jsonschema: unable to determine the JSON Schema type for the given schema")
	}
	return nil, inferred, nullable, nil
}

// unionUnsupportedKeywords are the single-kind keywords a union may not carry.
// The message names them in the order they are listed here, so it reads the
// same on every run.
var unionUnsupportedKeywords = []string{
	"minLength", "maxLength", "pattern", "format",
	"minimum", "maximum", "multipleOf",
	"items", "minItems", "maxItems", "uniqueItems",
	"properties", "required", "additionalProperties",
}

// ensureUnionConstraintsAreSupported refuses a multi-type union that also
// carries a keyword belonging to one kind.
//
// A union renders as a list of names and nothing else, so a minLength beside it
// would be dropped on the way in and absent on the way out -- a document that
// silently means less than it says.
func ensureUnionConstraintsAreSupported(schema map[string]any) error {
	var unsupported []string
	for _, keyword := range unionUnsupportedKeywords {
		// array_keys, so presence and not isset: a keyword set to null is still
		// a keyword the union cannot carry.
		if _, ok := schema[keyword]; ok {
			unsupported = append(unsupported, keyword)
		}
	}
	if len(unsupported) == 0 {
		return nil
	}
	return fmt.Errorf("jsonschema: type-specific keywords [%s] are not supported on a multi-type JSON Schema union", strings.Join(unsupported, ", "))
}

// inferType names the kind when "type" is absent but the shape says it anyway.
//
// The order is load-bearing: properties before items
// before enum before the string keywords before the numeric ones, so a fragment
// carrying two families of keyword resolves the same way every time.
func inferType(schema map[string]any) (string, bool) {
	switch {
	case isset(schema, "properties"), isset(schema, "additionalProperties"), isset(schema, "required"):
		return "object", true
	case isset(schema, "items"), isset(schema, "minItems"), isset(schema, "maxItems"), isset(schema, "uniqueItems"):
		return "array", true
	case isset(schema, "enum"):
		if values, ok := objectEntries(schema["enum"]); ok {
			return inferEnumType(values)
		}
		return "", false
	case isset(schema, "minLength"), isset(schema, "maxLength"), isset(schema, "pattern"), isset(schema, "format"):
		return "string", true
	case isset(schema, "minimum"), isset(schema, "maximum"), isset(schema, "multipleOf"):
		return "number", true
	}
	return "", false
}

// inferEnumType names the kind an enum of scalars shares.
//
// A mixed enum has no single kind and is refused rather than widened, except
// for whole and fractional numbers, which together are "number".
//
// A JSON decoder gives every number as a float64, so whole and fractional are
// told apart here by asking whether the value has a fractional part. An enum
// written with a trailing zero -- [1.0, 2.0] -- is therefore "integer". Nothing
// validates differently for it, because the enum already refuses every value
// the narrower kind would have.
func inferEnumType(values []entry) (string, bool) {
	resolved := ""
	for _, v := range values {
		var current string
		switch value := v.value.(type) {
		case bool:
			current = "boolean"
		case string:
			current = "string"
		case float64:
			if value == math.Trunc(value) {
				current = "integer"
			} else {
				current = "number"
			}
		default:
			if n, ok := asNumber(v.value); ok {
				if n == math.Trunc(n) {
					current = "integer"
				} else {
					current = "number"
				}
				break
			}
			return "", false
		}

		if resolved == "" || resolved == current {
			resolved = current
			continue
		}
		if isNumeric(resolved) && isNumeric(current) {
			resolved = "number"
			continue
		}
		return "", false
	}
	if resolved == "" {
		return "", false
	}
	return resolved, true
}

func isNumeric(kind string) bool {
	return kind == "integer" || kind == "number"
}

// normalizeUnions collapses a nullable "anyOf" or "oneOf" into the branch it
// wraps.
//
// It is the one composition keyword this subset reads, and only in the shape a
// generator emits for an optional value: exactly one real branch beside a
// "null" one. Anything else is a genuine union of schemas, which [AnyOfType]
// can hold but the named types cannot, so it is refused.
//
// The bool is whether the "null" branch was there, which the caller turns into
// nullable on the type it builds.
func (d *deserializer) normalizeUnions(schema map[string]any, refs []string) (map[string]any, bool, []string, error) {
	for _, key := range []string{"anyOf", "oneOf"} {
		raw, ok := schema[key]
		if !ok || raw == nil {
			continue
		}
		listed, ok := objectEntries(raw)
		if !ok {
			continue
		}

		nullable := false
		var kept []map[string]any
		var keptRefs [][]string
		for _, item := range listed {
			branch, ok := item.value.(map[string]any)
			if !ok {
				continue
			}
			branch, branchRefs, err := d.resolveRef(branch, refs)
			if err != nil {
				return nil, false, nil, err
			}
			if isNullBranch(branch) {
				nullable = true
				continue
			}
			kept = append(kept, branch)
			keptRefs = append(keptRefs, branchRefs)
		}

		if !nullable || len(kept) != 1 {
			return nil, false, nil, fmt.Errorf("jsonschema: only a nullable %q (a single schema plus a \"null\" branch) is supported", key)
		}

		branch := kept[0]
		siblings := make(map[string]any, len(schema))
		for k, v := range schema {
			if k != key {
				siblings[k] = v
			}
		}

		// The keys are sorted so that a fragment with two conflicts names the
		// same one on every run. A Go map has no order, and an error message
		// that moves is an error message nobody can test.
		names := make([]string, 0, len(siblings))
		for k := range siblings {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if other, ok := branch[k]; ok && !reflect.DeepEqual(other, siblings[k]) {
				return nil, false, nil, fmt.Errorf("jsonschema: conflicting [%s] between a %q branch and its sibling keys", k, key)
			}
		}

		// array_merge: the branch wins where both carry a key, which after the
		// conflict check above can only be a key they already agree on.
		merged := make(map[string]any, len(siblings)+len(branch))
		for k, v := range siblings {
			merged[k] = v
		}
		for k, v := range branch {
			merged[k] = v
		}
		return merged, true, keptRefs[0], nil
	}
	return schema, false, refs, nil
}

// isNullBranch reports whether a branch describes only the null type.
func isNullBranch(branch map[string]any) bool {
	switch t := branch["type"].(type) {
	case string:
		return t == "null"
	case []any:
		if len(t) != 1 {
			return false
		}
		name, ok := t[0].(string)
		return ok && name == "null"
	}
	return false
}

// resolveRef follows a local "$ref" and merges the sibling keys over its
// target.
//
// The target is resolved first and the siblings are laid over it, so a fragment
// that refers to a definition and adds a description keeps its own description.
// It recurses because a target may itself be a reference, and the accumulated
// list is what catches a cycle.
func (d *deserializer) resolveRef(schema map[string]any, refs []string) (map[string]any, []string, error) {
	raw, ok := schema["$ref"]
	if !ok {
		return schema, refs, nil
	}
	ref, ok := raw.(string)
	if !ok {
		return schema, refs, nil
	}

	for _, seen := range refs {
		if seen == ref {
			return nil, nil, fmt.Errorf("jsonschema: circular JSON Schema $ref [%s] detected", ref)
		}
	}
	// A fresh list, so every branch of an anyOf gets its own copy of the
	// trail that led to it. Appending in place
	// would make two sibling branches share a backing array and report a cycle
	// that is not one.
	refs = append(append(make([]string, 0, len(refs)+1), refs...), ref)

	resolved, err := d.lookupRef(ref)
	if err != nil {
		return nil, nil, err
	}

	merged := make(map[string]any, len(resolved)+len(schema))
	for k, v := range resolved {
		merged[k] = v
	}
	for k, v := range schema {
		if k != "$ref" {
			merged[k] = v
		}
	}
	return d.resolveRef(merged, refs)
}

// lookupRef reads a local JSON pointer out of the root document.
//
// Only local pointers resolve. Fetching a remote schema would make deserializing
// a document a network call, which is a request an attacker chooses the target
// of.
func (d *deserializer) lookupRef(ref string) (map[string]any, error) {
	if cached, ok := d.refCache[ref]; ok {
		return cached, nil
	}
	if ref == "#" {
		d.refCache[ref] = d.root
		return d.root, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("jsonschema: unable to resolve non-local JSON Schema $ref [%s]", ref)
	}

	var target any = d.root
	for _, segment := range strings.Split(ref[2:], "/") {
		next, ok := pointerIndex(target, unescapePointer(segment))
		if !ok {
			return nil, fmt.Errorf("jsonschema: unable to resolve JSON Schema $ref [%s]", ref)
		}
		target = next
	}

	// A list reaching here would go on to fail with "unable to determine the
	// JSON Schema type". The answer is the same, and this message says which
	// of the two happened.
	object, ok := target.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jsonschema: the JSON Schema $ref [%s] does not point to a schema", ref)
	}
	d.refCache[ref] = object
	return object, nil
}

// unescapePointer decodes one JSON pointer segment: percent-escapes first,
// then the two pointer escapes.
//
// The order matters and is the one RFC 6901 states: ~1 becomes a slash before
// ~0 becomes a tilde, so "~01" reads as "~1" and not as a slash.
func unescapePointer(segment string) string {
	// rawurldecode leaves an invalid escape alone rather than failing, and a
	// segment is a key in someone's document, not a URL this package chose.
	if decoded, err := url.PathUnescape(segment); err == nil {
		segment = decoded
	}
	segment = strings.ReplaceAll(segment, "~1", "/")
	return strings.ReplaceAll(segment, "~0", "~")
}

// pointerIndex reads one pointer segment out of one level of the document. A
// list is indexed by position, so "0" reaches its first element.
func pointerIndex(target any, segment string) (any, bool) {
	switch t := target.(type) {
	case map[string]any:
		v, ok := t[segment]
		return v, ok
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(t) {
			return nil, false
		}
		return t[i], true
	}
	return nil, false
}

// toNumber normalizes a keyword's value to a float64, or reports that it is
// not a number.
//
// A numeric string counts, because a schema written by hand often quotes its
// bounds.
func toNumber(v any) (float64, bool) {
	if n, ok := asNumber(v); ok {
		return n, true
	}
	if s, ok := v.(string); ok {
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return n, err == nil
	}
	return 0, false
}

// toInteger narrows a bound to a whole number, refusing one that is not.
//
// Converting an out-of-range float64 to an int is undefined in Go, so a bound
// larger than an int is an error rather than whichever number the conversion
// happens to produce.
func toInteger(v float64) (int, error) {
	if math.Floor(v) != v {
		return 0, fmt.Errorf("jsonschema: the JSON Schema integer constraint [%s] must be an integer", number(v))
	}
	if v < math.MinInt || v > math.MaxInt {
		return 0, fmt.Errorf("jsonschema: the JSON Schema integer constraint [%s] is out of range", number(v))
	}
	return int(v), nil
}

// entry is one member of a decoded JSON object, or one element of a decoded
// list. The deserializer walks several keywords -- properties, enum, required,
// anyOf -- that a document may legitimately write either way.
type entry struct {
	key   string
	value any
}

// objectEntries reads the members of a decoded JSON container, and reports
// whether it was one.
//
// The members of an object come back ordered by name: a Go map has no order,
// and a schema whose properties land in a different order on every run is a
// schema nobody can diff. A list keeps its own order, and its indices become
// the keys.
func objectEntries(v any) ([]entry, bool) {
	switch t := v.(type) {
	case map[string]any:
		names := make([]string, 0, len(t))
		for name := range t {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]entry, 0, len(t))
		for _, name := range names {
			out = append(out, entry{key: name, value: t[name]})
		}
		return out, true
	case []any:
		out := make([]entry, 0, len(t))
		for i, item := range t {
			out = append(out, entry{key: strconv.Itoa(i), value: item})
		}
		return out, true
	}
	return nil, false
}

// requiredNames reads an object's "required" list into the set buildObject
// tests each property against. The names are read as strings.
func requiredNames(v any) map[string]bool {
	listed, ok := objectEntries(v)
	if !ok {
		return nil
	}
	out := make(map[string]bool, len(listed))
	for _, item := range listed {
		out[phpString(item.value)] = true
	}
	return out
}

// isset reports that the key is there and its value is not null. A couple of
// keywords are read for presence alone instead, and the difference decides what
// a null does.
func isset(schema map[string]any, key string) bool {
	v, ok := schema[key]
	return ok && v != nil
}

// isEmptyContainer reports whether a decoded value is an empty container, which
// both [] and {} decode to. "items" is compared against it before the value is
// read as a tuple.
func isEmptyContainer(v any) bool {
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// intKeyword reads a keyword as a whole number, reporting whether it was there
// at all. minItems, maxItems, minLength and maxLength are all read this way.
func intKeyword(schema map[string]any, key string) (int, bool) {
	raw, ok := schema[key]
	if !ok || raw == nil {
		return 0, false
	}
	return phpInt(raw), true
}

// phpInt coerces the values a JSON decoder produces to an int: a number
// truncates toward zero, a numeric string parses, and a bool is one or nothing.
// Anything else is zero.
func phpInt(v any) int {
	if n, ok := toNumber(v); ok {
		switch {
		case n < math.MinInt:
			return math.MinInt
		case n > math.MaxInt:
			return math.MaxInt
		}
		return int(math.Trunc(n))
	}
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 0
}

// phpString coerces the values a JSON decoder produces to a string. It is what
// title, description, pattern, format and the "type" names are read with, so a
// document that wrote one of them as a number is still readable.
func phpString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return ""
	case nil:
		return ""
	}
	if n, ok := asNumber(v); ok {
		return number(n)
	}
	return fmt.Sprint(v)
}

// truthy coerces a decoded value to a bool, and is what uniqueItems is read
// with. Zero, the empty string, the string "0" and an empty container are all
// false.
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	case nil:
		return false
	}
	if n, ok := asNumber(v); ok {
		return n != 0
	}
	return true
}
