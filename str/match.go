package str

import "strings"

// Is reports whether value matches pattern, where * stands for any run of
// characters, including none. Every other rune is literal, so a pattern is safe
// to build from configuration without escaping anything.
//
// Is("admin/*", "admin/users") is true, and so is Is("*", anything).
//
// It is not path.Match: * crosses a slash, because what this matches is a route
// name, an ability or a host, not a file name.
func Is(pattern, value string) bool {
	if pattern == value {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return false
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	rest := value[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}
