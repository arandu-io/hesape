package http

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// dataGet answers to the global data_get() helper that Illuminate\Http leans on
// in input(), json() and all of Illuminate\Support\Traits\InteractsWithData. It
// walks a "dot.separated" key through maps and slices and returns def when any
// segment is absent.
func dataGet(target any, key string, def any) any {
	if key == "" {
		return target
	}

	current := target

	for _, segment := range strings.Split(key, ".") {
		next, ok := dataGetSegment(current, segment)
		if !ok {
			return def
		}

		current = next
	}

	return current
}

func dataGetSegment(target any, segment string) (any, bool) {
	switch typed := target.(type) {
	case nil:
		return nil, false
	case map[string]any:
		value, ok := typed[segment]

		return value, ok
	case map[string]string:
		value, ok := typed[segment]
		if !ok {
			return nil, false
		}

		return value, true
	case map[string][]string:
		value, ok := typed[segment]
		if !ok {
			return nil, false
		}

		return value, true
	case []any:
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(typed) {
			return nil, false
		}

		return typed[index], true
	case []string:
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(typed) {
			return nil, false
		}

		return typed[index], true
	}

	return nil, false
}

// dataSet answers to data_set()/Arr::set(): it writes value at a "dot.separated"
// key, creating the intermediate maps the path needs.
func dataSet(target map[string]any, key string, value any) {
	if key == "" {
		return
	}

	segments := strings.Split(key, ".")
	current := target

	for i, segment := range segments {
		if i == len(segments)-1 {
			current[segment] = value

			return
		}

		next, ok := current[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[segment] = next
		}

		current = next
	}
}

// arrHas answers to Arr::has(): true only when every segment of the dotted key
// is present, including when the value found is nil.
func arrHas(target any, key string) bool {
	if key == "" {
		return false
	}

	current := target

	for _, segment := range strings.Split(key, ".") {
		next, ok := dataGetSegment(current, segment)
		if !ok {
			return false
		}

		current = next
	}

	return true
}

// arrHasAny answers to Arr::hasAny(): true when at least one key is present.
func arrHasAny(target any, keys []string) bool {
	for _, key := range keys {
		if arrHas(target, key) {
			return true
		}
	}

	return false
}

// arrForget answers to Arr::forget(): removes every dotted key from the map.
func arrForget(target map[string]any, keys []string) {
	for _, key := range keys {
		if key == "" {
			continue
		}

		segments := strings.Split(key, ".")
		current := target

		for i, segment := range segments {
			if i == len(segments)-1 {
				delete(current, segment)

				break
			}

			next, ok := current[segment].(map[string]any)
			if !ok {
				break
			}

			current = next
		}
	}
}

// strIs answers to Str::is(): "*" is the only wildcard and the match is anchored.
func strIs(pattern, value string) bool {
	if pattern == value {
		return true
	}

	if pattern == "*" {
		return true
	}

	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, `\*`, ".*")

	matched, err := regexp.MatchString("^"+quoted+"$", value)

	return err == nil && matched
}

// strContainsAny answers to Str::contains() with an array of needles.
func strContainsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(haystack, needle) {
			return true
		}
	}

	return false
}

// strAfter answers to Str::after().
func strAfter(subject, search string) string {
	if search == "" {
		return subject
	}

	index := strings.Index(subject, search)
	if index < 0 {
		return subject
	}

	return subject[index+len(search):]
}

// strBefore answers to Str::before().
func strBefore(subject, search string) string {
	if search == "" {
		return subject
	}

	index := strings.Index(subject, search)
	if index < 0 {
		return subject
	}

	return subject[:index]
}

// strSnake answers to Str::snake(), used by RedirectResponse's dynamic withX().
func strSnake(value string) string {
	var builder strings.Builder

	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				builder.WriteByte('_')
			}

			builder.WriteRune(r - 'A' + 'a')

			continue
		}

		builder.WriteRune(r)
	}

	return builder.String()
}

const randomAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// strRandom answers to Str::random(), used by FileHelpers::hashName().
func strRandom(length int) string {
	if length <= 0 {
		return ""
	}

	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}

	for i, b := range buffer {
		buffer[i] = randomAlphabet[int(b)%len(randomAlphabet)]
	}

	return string(buffer)
}

// stringify answers to PHP's (string) cast for the scalar values that reach
// input(): it is how filled(), boolean() and integer() read a value.
func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case bool:
		if typed {
			return "1"
		}

		return ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}

		return ""
	case []any:
		if len(typed) > 0 {
			return stringify(typed[0])
		}

		return ""
	}

	return fmt.Sprint(value)
}
