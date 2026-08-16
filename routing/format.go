package routing

import (
	"fmt"
	"sort"
	"strings"
)

// FormatRoutes renders the route table for the terminal, grouped by module and
// sorted by pattern.
//
// It is here, and not in the CLI, so that every consumer prints the same
// table: the CLI's route list and the route list on the error page are the
// same rows in the same order, and a developer comparing the two is comparing
// like with like.
//
// The name column is what a developer copies into Routes.Route instead of
// typing the path, so a route without one prints short rather than printing an
// empty column that looks like a value.
func FormatRoutes(routes []*Route) string {
	sorted := append([]*Route{}, routes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Module != sorted[j].Module {
			return sorted[i].Module < sorted[j].Module
		}
		if sorted[i].Pattern != sorted[j].Pattern {
			return sorted[i].Pattern < sorted[j].Pattern
		}
		return sorted[i].Method < sorted[j].Method
	})

	var b strings.Builder
	module := ""
	for _, r := range sorted {
		if r.Module != module {
			module = r.Module
			fmt.Fprintf(&b, "\n%s\n", module)
		}
		if name := r.RouteName(); name != "" {
			fmt.Fprintf(&b, "  %-7s %-34s %s\n", r.Method, r.Pattern, name)
		} else {
			fmt.Fprintf(&b, "  %-7s %s\n", r.Method, r.Pattern)
		}
	}
	if b.Len() == 0 {
		return "no routes registered\n"
	}
	return strings.TrimLeft(b.String(), "\n")
}
