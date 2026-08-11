package routing

import (
	"strings"
)

// MergeGroupAttributes merges new group attributes into old, returning the
// combined set. It is the Go shape of Laravel's RouteGroup::merge, for the
// group stack the router does not keep (here a sub-router is created per
// Group, so the merge is for the registrar and the URL generator, which read
// attributes rather than hold a stack).
//
// The fields merged are prefix (concatenated with /), name (concatenated with
// a dot), namespace (concatenated with \), where (union, new wins), and
// middleware (old then new). Domain and controller replace rather than merge,
// which is what Laravel does for them.
func MergeGroupAttributes(new, old map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range old {
		out[k] = v
	}

	if v, ok := new["prefix"].(string); ok {
		out["prefix"] = formatPrefix(v, getString(out, "prefix"))
	} else if v, ok := new["prefix"]; ok {
		out["prefix"] = v
	}

	if v, ok := new["as"].(string); ok {
		out["as"] = formatAs(v, getString(out, "as"))
	}

	if v, ok := new["namespace"].(string); ok {
		out["namespace"] = formatNamespace(v, getString(out, "namespace"))
	}

	if v, ok := new["where"].(map[string]string); ok {
		old := map[string]string{}
		if existing, ok := out["where"].(map[string]string); ok {
			old = existing
		}
		out["where"] = formatWhere(v, old)
	}

	if v, ok := new["domain"].(string); ok {
		out["domain"] = v
	}

	if v, ok := new["controller"].(string); ok {
		out["controller"] = v
	}

	return out
}

// formatPrefix concatenates new onto old with a slash, trimming slashes on
// both sides.
func formatPrefix(new, old string) string {
	new = strings.Trim(new, "/")
	old = strings.Trim(old, "/")
	if new == "" {
		return old
	}
	if old == "" {
		return new
	}
	return old + "/" + new
}

// formatAs concatenates new onto old literally, which is what Laravel does:
// the dot is the caller's to remember in as, and the registrar's Name joins
// with a dot instead.
func formatAs(new, old string) string {
	return old + new
}

// formatNamespace concatenates new onto old with a backslash, unless new is
// absolute (starts with \).
func formatNamespace(new, old string) string {
	new = strings.Trim(new, "\\")
	if new == "" {
		return old
	}
	if strings.HasPrefix(new, "\\") {
		return strings.Trim(new, "\\")
	}
	if old == "" {
		return new
	}
	return strings.Trim(old, "\\") + "\\" + new
}

// formatWhere unions new into old, new winning on conflict.
func formatWhere(new, old map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range old {
		out[k] = v
	}
	for k, v := range new {
		out[k] = v
	}
	return out
}

// getString reads a string field from a map, or empty.
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
