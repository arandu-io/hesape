package concerns

import (
	"strings"
	"sync"
)

// GuardsAttributes answers
// Illuminate\Database\Eloquent\Concerns\GuardsAttributes: which attributes may
// be set from a payload the application did not write.
//
// It is the difference between fill and forceFill, and the reason it exists is
// the request body: an update that copies every field of a form into a model
// will happily copy is_admin if nothing says otherwise. The default guard is
// ["*"], so a model that declares nothing is fully guarded and mass assignment
// does nothing at all -- refusing is the safe direction to fail.
type GuardsAttributes struct {
	fillable []string
	guarded  []string
}

// unguarded answers GuardsAttributes::$unguarded, the global switch a seeder
// flips. It is behind a mutex because it is global and a seeder is not always
// the only thing running.
var unguarded = struct {
	sync.RWMutex
	on bool
}{}

// GetFillable answers GuardsAttributes::getFillable.
func (g *GuardsAttributes) GetFillable() []string { return g.fillable }

// Fillable answers GuardsAttributes::fillable.
func (g *GuardsAttributes) Fillable(fillable []string) { g.fillable = fillable }

// MergeFillable answers GuardsAttributes::mergeFillable.
func (g *GuardsAttributes) MergeFillable(fillable []string) {
	g.fillable = append(g.fillable, fillable...)
}

// GetGuarded answers GuardsAttributes::getGuarded. The default is ["*"].
func (g *GuardsAttributes) GetGuarded() []string {
	if g.guarded == nil {
		return []string{"*"}
	}
	return g.guarded
}

// Guard answers GuardsAttributes::guard.
func (g *GuardsAttributes) Guard(guarded []string) { g.guarded = guarded }

// MergeGuarded answers GuardsAttributes::mergeGuarded.
func (g *GuardsAttributes) MergeGuarded(guarded []string) {
	g.guarded = append(g.GetGuarded(), guarded...)
}

// Unguard answers GuardsAttributes::unguard.
func (g *GuardsAttributes) Unguard(state ...bool) { Unguard(state...) }

// Unguard answers the static GuardsAttributes::unguard.
//
// Static in PHP; a package function here, plus a method on the struct so that
// both spellings reach the same switch. Two switches would be one switch
// somebody forgot to turn back on.
func Unguard(state ...bool) {
	unguarded.Lock()
	defer unguarded.Unlock()
	unguarded.on = len(state) == 0 || state[0]
}

// Reguard answers GuardsAttributes::reguard.
func Reguard() { Unguard(false) }

// IsUnguarded answers GuardsAttributes::isUnguarded.
func IsUnguarded() bool {
	unguarded.RLock()
	defer unguarded.RUnlock()
	return unguarded.on
}

// Unguarded answers GuardsAttributes::unguarded: the callback runs with the
// guard off, and it goes back on afterwards even if the callback fails.
func Unguarded(callback func() error) error {
	if IsUnguarded() {
		return callback()
	}

	Unguard()
	defer Reguard()
	return callback()
}

// IsFillable answers GuardsAttributes::isFillable.
func (g *GuardsAttributes) IsFillable(key string) bool {
	if IsUnguarded() {
		return true
	}
	if contains(g.fillable, key) {
		return true
	}
	if g.IsGuarded(key) {
		return false
	}

	// The PHP's last line: with no fillable list, anything that is not guarded
	// and does not look like a nested json path is fillable.
	return len(g.fillable) == 0 && !strings.Contains(key, ".")
}

// IsGuarded answers GuardsAttributes::isGuarded.
func (g *GuardsAttributes) IsGuarded(key string) bool {
	guarded := g.GetGuarded()
	if len(guarded) == 0 {
		return false
	}
	if contains(guarded, "*") {
		return true
	}
	return contains(guarded, strings.ToLower(key)) || contains(guarded, key)
}

// TotallyGuarded answers GuardsAttributes::totallyGuarded: nothing may be mass
// assigned at all.
func (g *GuardsAttributes) TotallyGuarded() bool {
	return len(g.fillable) == 0 && contains(g.GetGuarded(), "*")
}

// FillableFromArray answers GuardsAttributes::fillableFromArray: the payload,
// narrowed to what the model allows.
func (g *GuardsAttributes) FillableFromArray(attributes map[string]any) map[string]any {
	if len(g.fillable) == 0 || IsUnguarded() {
		return attributes
	}

	out := make(map[string]any, len(attributes))
	for key, value := range attributes {
		if contains(g.fillable, key) {
			out[key] = value
		}
	}
	return out
}
