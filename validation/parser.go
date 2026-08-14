package validation

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// This file answers to Illuminate\Validation\ValidationRuleParser and
// Illuminate\Validation\ValidationData: how a rule string becomes a name and its
// parameters, and how "items.*.price" becomes one rule per item the request
// actually sent.
//
// Compile does the same walk at boot for a rule set written in a package-level
// variable. These are the same steps as a value, for the caller that builds a
// rule set from something that is not a literal.
//
// The wildcard half is NOT boot work and cannot be: "items.*.price" means the
// items this request sent, so Validator.explodeRules calls
// explodeWildcardRules once per request, over the compiled set. It called
// nothing at all until an audit proved a wildcard rule set inert.

// ExplodedRules answers to the stdClass ValidationRuleParser::explode returns:
// the rules with every wildcard expanded, and the wildcard keys that produced
// them.
type ExplodedRules struct {
	// Rules answers to ->rules: one entry per attribute, holding the rules
	// written against it in the order they were written.
	Rules map[string][]string

	// Order is the order the attributes were written in. A PHP array remembers
	// it and a Go map remembers none.
	Order []string

	// ImplicitAttributes answers to ->implicitAttributes: the wildcard key that
	// each expanded attribute came from, which is what getPrimaryAttribute reads
	// to name a field in a message.
	ImplicitAttributes map[string][]string
}

// ValidationRuleParser answers to Illuminate\Validation\ValidationRuleParser.
type ValidationRuleParser struct {
	// Data answers to the public $data: the request the wildcards are expanded
	// against, because "items.*" means the items that were actually sent.
	Data Data

	// ImplicitAttributes answers to the public $implicitAttributes.
	ImplicitAttributes map[string][]string
}

// NewValidationRuleParser answers to the ValidationRuleParser constructor.
func NewValidationRuleParser(data Data) *ValidationRuleParser {
	return &ValidationRuleParser{Data: data, ImplicitAttributes: map[string][]string{}}
}

// Explode answers to ValidationRuleParser::explode: the human-friendly rules
// turned into the full rules array the validator runs.
//
// The primary purpose of this parser is to expand any "*" rules to all of the
// explicit rules needed for the given data. For example the rule names.* would
// get expanded to names.0, names.1, and so on.
func (p *ValidationRuleParser) Explode(rules Rules) *ExplodedRules {
	p.ImplicitAttributes = map[string][]string{}

	exploded := &ExplodedRules{Rules: map[string][]string{}}

	for _, attribute := range slices.Sorted(maps.Keys(rules)) {
		if !strings.Contains(attribute, "*") {
			p.mergeInto(exploded, attribute, splitChain(rules[attribute]))
			continue
		}
		for _, key := range p.explodeWildcardRules(attribute) {
			p.ImplicitAttributes[attribute] = append(p.ImplicitAttributes[attribute], key)
			p.mergeInto(exploded, key, splitChain(rules[attribute]))
		}
	}

	exploded.ImplicitAttributes = p.ImplicitAttributes

	return exploded
}

func (p *ValidationRuleParser) mergeInto(exploded *ExplodedRules, attribute string, rules []string) {
	if _, held := exploded.Rules[attribute]; !held {
		exploded.Order = append(exploded.Order, attribute)
	}
	exploded.Rules[attribute] = append(exploded.Rules[attribute], rules...)
}

// explodeWildcardRules answers to
// ValidationRuleParser::explodeWildcardRules: the keys of the data that one
// wildcard attribute names, sorted so that two runs agree.
func (p *ValidationRuleParser) explodeWildcardRules(attribute string) []string {
	pattern := strings.ReplaceAll(regexp.QuoteMeta(attribute), `\*`, `[^.]*`)

	compiled, err := regexp.Compile(`^` + pattern + `$`)
	if err != nil {
		return nil
	}

	gathered := InitializeAndGatherData(attribute, p.Data)

	var keys []string
	for _, key := range slices.Sorted(maps.Keys(gathered)) {
		if strings.HasPrefix(key, attribute) || compiled.MatchString(key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// MergeRules answers to ValidationRuleParser::mergeRules: more rules on an
// attribute that may already have some.
func (p *ValidationRuleParser) MergeRules(results Rules, attribute, rules string) Rules {
	if results == nil {
		results = Rules{}
	}
	if existing, held := results[attribute]; held && existing != "" && rules != "" {
		results[attribute] = existing + "|" + rules
		return results
	}
	if rules != "" {
		results[attribute] = rules
	}
	return results
}

// Parse answers to the static ValidationRuleParser::parse: a rule string split
// into its name and the parameters written after the colon.
//
// The PHP studlies the name -- "date_format" becomes "DateFormat" -- so that it
// can build a method name out of it. There is no method name to build here, so
// the name stays as it was typed, which is how every table in this package keys
// it. Its two aliases are normalized as the PHP normalizes them: "int" is
// "integer" and "bool" is "boolean".
//
// A malformed parameter list answers with no parameters rather than an error;
// Compile is where a malformed list is reported, with the field and the file.
func Parse(rule string) (string, []string) {
	name, rest, hasArgs := strings.Cut(rule, ":")
	name = normalizeRule(strings.TrimSpace(name))

	parameters, err := parseArgs(name, rest, hasArgs)
	if err != nil {
		return name, nil
	}
	return name, parameters
}

// normalizeRule answers to ValidationRuleParser::normalizeRule.
func normalizeRule(rule string) string {
	switch rule {
	case "int":
		return "integer"
	case "bool":
		return "boolean"
	}
	return rule
}

// FilterConditionalRules answers to the static
// ValidationRuleParser::filterConditionalRules: every ConditionalRules in the
// set replaced by the rules its condition chose.
func FilterConditionalRules(rules map[string]any, data Data) Rules {
	out := Rules{}

	for attribute, written := range rules {
		switch typed := written.(type) {
		case string:
			out[attribute] = typed
		case *ConditionalRules:
			if typed.Passes(data) {
				out[attribute] = strings.Join(typed.Rules(data), "|")
				continue
			}
			out[attribute] = strings.Join(typed.DefaultRules(data), "|")
		case []string:
			out[attribute] = strings.Join(typed, "|")
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Illuminate\Validation\ConditionalRules.
// ---------------------------------------------------------------------------

// ConditionalRules answers to Illuminate\Validation\ConditionalRules: the rules
// an attribute gets when a condition holds, and the ones it gets when it does
// not.
type ConditionalRules struct {
	// condition answers to $condition. The PHP takes a bool or a callable; the
	// callable is the general case and NewConditionalRules wraps a bool in one.
	condition func(Data) bool

	rules        []string
	defaultRules []string
}

// NewConditionalRules answers to the ConditionalRules constructor.
func NewConditionalRules(condition func(Data) bool, rules string, defaultRules ...string) *ConditionalRules {
	c := &ConditionalRules{condition: condition, rules: splitChain(rules)}
	if len(defaultRules) > 0 && defaultRules[0] != "" {
		c.defaultRules = splitChain(defaultRules[0])
	}
	return c
}

// Passes answers to ConditionalRules::passes.
func (c *ConditionalRules) Passes(data Data) bool {
	if c.condition == nil {
		return false
	}
	return c.condition(data)
}

// Rules answers to ConditionalRules::rules.
func (c *ConditionalRules) Rules(data Data) []string { return c.rules }

// DefaultRules answers to ConditionalRules::defaultRules.
func (c *ConditionalRules) DefaultRules(data Data) []string { return c.defaultRules }

// ---------------------------------------------------------------------------
// Illuminate\Validation\NestedRules.
// ---------------------------------------------------------------------------

// NestedRules answers to Illuminate\Validation\NestedRules: the rules for one
// member of an array, decided by looking at that member.
type NestedRules struct {
	callback func(value any, attribute string, data Data) Rules
}

// NewNestedRules answers to the NestedRules constructor.
func NewNestedRules(callback func(value any, attribute string, data Data) Rules) *NestedRules {
	return &NestedRules{callback: callback}
}

// Compile answers to NestedRules::compile.
func (n *NestedRules) Compile(attribute string, value any, data Data) *ExplodedRules {
	if n.callback == nil {
		return &ExplodedRules{Rules: map[string][]string{}, ImplicitAttributes: map[string][]string{}}
	}
	return NewValidationRuleParser(data).Explode(n.callback(value, attribute, data))
}

// ---------------------------------------------------------------------------
// Illuminate\Validation\ValidationData.
// ---------------------------------------------------------------------------

// InitializeAndGatherData answers to
// ValidationData::initializeAndGatherData: the slice of the request one
// attribute names, flattened, plus the exact keys a wildcard matched.
func InitializeAndGatherData(attribute string, masterData Data) map[string]any {
	initialized := initializeAttributeOnData(attribute, masterData)

	gathered := arrDot(initialized)

	maps.Copy(gathered, extractValuesForWildcards(masterData, gathered, attribute))

	return gathered
}

// initializeAttributeOnData answers to
// ValidationData::initializeAttributeOnData.
//
// The data_set call is the whole point of the method, and it was missing: a
// wildcard attribute NOBODY SENT A VALUE FOR still has to produce a key, or the
// rules written against it never run. With "foo.*.bar" against
// {"foo": [{"baz": "x"}]} the flattened data holds only foo.0.baz, so
// explodeWildcardRules found no key, the field expanded to nothing, and
// "foo.*.bar": "required_with:foo.*.baz" passed on a request that PHP fails.
// Filling foo.0.bar with null is what puts the key there.
//
// The slice of the request is deep-copied first. A PHP array is a value and
// copies itself on write; a Go map and a Go slice are references, so writing
// the null through would write it into the request's own data.
func initializeAttributeOnData(attribute string, masterData Data) Data {
	explicitPath := GetLeadingExplicitAttributePath(attribute)

	data, _ := deepClone(ExtractDataFromPath(explicitPath, masterData)).(Data)

	if !strings.Contains(attribute, "*") || strings.HasSuffix(attribute, "*") {
		return data
	}

	filled, _ := dataSet(data, strings.Split(attribute, "."), nil).(Data)

	return filled
}

// dataSet answers to the data_set helper as ValidationData calls it: write the
// value at the dotted path, walking every member where the path says "*", and
// making the levels it needs on the way.
//
// The PHP passes $overwrite = true, which is safe there and here for the same
// reason: the target is the throwaway copy this gathers KEYS out of, and the
// values are read back from the request by extractValuesForWildcards.
func dataSet(target any, segments []string, value any) any {
	if len(segments) == 0 {
		return value
	}
	segment, rest := segments[0], segments[1:]

	if segment == "*" {
		switch node := target.(type) {
		case Data:
			for key, inner := range node {
				node[key] = dataSet(inner, rest, value)
			}
			return node
		case map[string]any:
			return dataSet(Data(node), segments, value)
		case []any:
			for i, inner := range node {
				node[i] = dataSet(inner, rest, value)
			}
			return node
		}
		// PHP: a target the wildcard cannot walk becomes an empty array, and
		// the loop over it writes nothing.
		return Data{}
	}

	switch node := target.(type) {
	case Data:
		if len(rest) == 0 {
			node[segment] = value
			return node
		}
		inner, held := node[segment]
		if !held {
			inner = Data{}
		}
		node[segment] = dataSet(inner, rest, value)
		return node
	case map[string]any:
		return dataSet(Data(node), segments, value)
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(node) {
			// PHP grows the array here. A request cannot address past the end
			// of what it sent, and inventing a member would invent a field.
			return node
		}
		if len(rest) == 0 {
			node[i] = value
			return node
		}
		node[i] = dataSet(node[i], rest, value)
		return node
	}

	// Not accessible: PHP replaces it with an array and writes into that.
	made := Data{}
	if len(rest) == 0 {
		made[segment] = value
		return made
	}
	made[segment] = dataSet(Data{}, rest, value)
	return made
}

// deepClone copies a slice of the request all the way down, so that dataSet
// cannot write through to the map the caller handed in.
func deepClone(value any) any {
	switch node := value.(type) {
	case Data:
		out := make(Data, len(node))
		for key, item := range node {
			out[key] = deepClone(item)
		}
		return out
	case map[string]any:
		out := make(Data, len(node))
		for key, item := range node {
			out[key] = deepClone(item)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, item := range node {
			out[i] = deepClone(item)
		}
		return out
	}
	return value
}

// extractValuesForWildcards answers to
// ValidationData::extractValuesForWildcards.
func extractValuesForWildcards(masterData Data, gathered map[string]any, attribute string) map[string]any {
	pattern := strings.ReplaceAll(regexp.QuoteMeta(attribute), `\*`, `[^.]+`)

	compiled, err := regexp.Compile(`^` + pattern)
	if err != nil {
		return map[string]any{}
	}

	out := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(gathered)) {
		if match := compiled.FindString(key); match != "" {
			out[match] = masterData.Get(match)
		}
	}
	return out
}

// ExtractDataFromPath answers to ValidationData::extractDataFromPath: the
// sub-section of the request one dotted path names, so that the rest of it is
// not walked.
func ExtractDataFromPath(attribute string, masterData Data) Data {
	results := Data{}

	if attribute == "" {
		maps.Copy(results, masterData)
		return results
	}

	if value, present := lookup(masterData, attribute); present {
		setPath(results, attribute, value)
	}

	return results
}

// GetLeadingExplicitAttributePath answers to
// ValidationData::getLeadingExplicitAttributePath: "foo.bar.*.baz" gives
// "foo.bar", which is all of the path that can be walked without guessing.
func GetLeadingExplicitAttributePath(attribute string) string {
	head, _, _ := strings.Cut(attribute, "*")

	return strings.TrimRight(head, ".")
}

// arrDot answers to Arr::dot over the shapes Data holds.
func arrDot(data Data) map[string]any {
	out := map[string]any{}
	flattenInto(out, "", data)
	return out
}

func flattenInto(out map[string]any, prefix string, value any) {
	switch node := value.(type) {
	case Data:
		if len(node) == 0 && prefix != "" {
			out[prefix] = node
			return
		}
		for key, item := range node {
			flattenInto(out, join(prefix, key), item)
		}
	case map[string]any:
		flattenInto(out, prefix, Data(node))
	case []any:
		if len(node) == 0 && prefix != "" {
			out[prefix] = node
			return
		}
		for i, item := range node {
			flattenInto(out, join(prefix, itoa(i)), item)
		}
	default:
		if prefix != "" {
			out[prefix] = value
		}
	}
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// setPath answers to Arr::set: write a value at a dotted path, making the levels
// it needs on the way.
func setPath(data Data, key string, value any) {
	segments := strings.Split(key, ".")

	current := data
	for _, segment := range segments[:len(segments)-1] {
		next, held := current[segment]
		if !held {
			made := Data{}
			current[segment] = made
			current = made
			continue
		}
		nested, isNested := asData(next)
		if !isNested {
			made := Data{}
			current[segment] = made
			current = made
			continue
		}
		current[segment] = nested
		current = nested
	}
	current[segments[len(segments)-1]] = value
}

// asData reads a value that is a nested array, in either spelling.
func asData(value any) (Data, bool) {
	switch typed := value.(type) {
	case Data:
		return typed, true
	case map[string]any:
		return Data(typed), true
	}
	return nil, false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for i > 0 {
		pos--
		digits[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(digits[pos:])
}
