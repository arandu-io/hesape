package resources

import "encoding/json"

// PotentiallyMissing is the interface that wraps the IsMissing method.
//
// Values that implement PotentiallyMissing are filtered out of JSON
// responses when IsMissing returns true. This is the Go equivalent of
// Illuminate's PotentiallyMissing interface.
type PotentiallyMissing interface {
	IsMissing() bool
}

// MergeValue mirrors Illuminate\Http\Resources\MergeValue.
//
// It wraps a value that should be merged into the parent array rather
// than nested as a separate key.
type MergeValue struct {
	Data any
}

// NewMergeValue creates a MergeValue.
func NewMergeValue(data any) *MergeValue {
	return &MergeValue{Data: data}
}

// MissingValue mirrors Illuminate\Http\Resources\MissingValue.
//
// It represents a value that should be omitted from the response.
type MissingValue struct{}

// IsMissing always returns true for MissingValue.
func (m MissingValue) IsMissing() bool { return true }

// MergeWhen creates a MergeValue if the condition is true, otherwise
// returns a MissingValue.
func MergeWhen(condition bool, value any) any {
	if condition {
		return NewMergeValue(value)
	}
	return MissingValue{}
}

// When mirrors Illuminate's ConditionallyLoadsAttributes::when.
//
// It returns value if condition is true, otherwise returns a MissingValue
// (which is filtered out during response serialization).
func When(condition bool, value any) any {
	if condition {
		return value
	}
	return MissingValue{}
}

// Unless returns value if condition is false, otherwise a MissingValue.
func Unless(condition bool, value any) any {
	return When(!condition, value)
}

// WhenNotNull returns value if it is non-nil, otherwise a MissingValue.
func WhenNotNull(value any) any {
	if value == nil {
		return MissingValue{}
	}
	return value
}

// WhenHas returns value if the map contains the given key.
func WhenHas(data map[string]any, key string, value any) any {
	if _, ok := data[key]; ok {
		if value != nil {
			return value
		}
		return data[key]
	}
	return MissingValue{}
}

// WhenLoaded returns value if the relation is loaded on the resource.
// The relation parameter is the relation name; loaded reports whether
// the relation has been loaded.
func WhenLoaded(relation string, loaded bool, value any) any {
	if loaded {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// WhenCounted returns value if the relation has been counted.
func WhenCounted(relation string, counted bool, value any) any {
	if counted {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// WhenAggregated returns value if the aggregate has been loaded.
func WhenAggregated(relation, column, aggregate string, loaded bool, value any) any {
	if loaded {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// WhenExistsLoaded returns value if the existence check has been loaded.
func WhenExistsLoaded(relation string, loaded bool, value any) any {
	if loaded {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// WhenPivotLoaded returns value if the pivot data is loaded for the
// given table.
func WhenPivotLoaded(table string, loaded bool, value any) any {
	if loaded {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// WhenPivotLoadedAs returns value if the pivot data is loaded under
// the given accessor.
func WhenPivotLoadedAs(accessor, table string, loaded bool, value any) any {
	return WhenPivotLoaded(table, loaded, value)
}

// WhenAppended returns value if the attribute has been appended.
func WhenAppended(attribute string, appended bool, value any) any {
	if appended {
		if value != nil {
			return value
		}
		return true
	}
	return MissingValue{}
}

// Transform applies a callback to value if value is not a MissingValue.
func Transform(value any, callback func(any) any) any {
	if _, ok := value.(MissingValue); ok {
		return MissingValue{}
	}
	return callback(value)
}

// Filter removes MissingValue entries from a map, and resolves
// MergeValue entries by merging their data into the parent.
func Filter(data map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		result = mergeValue(result, k, v)
	}
	return result
}

func mergeValue(data map[string]any, key string, value any) map[string]any {
	if _, ok := value.(MissingValue); ok {
		return data
	}
	if mv, ok := value.(*MergeValue); ok {
		if mv.Data == nil {
			return data
		}
		b, err := json.Marshal(mv.Data)
		if err != nil {
			return data
		}
		var merged map[string]any
		if err := json.Unmarshal(b, &merged); err != nil {
			return data
		}
		for mk, mv := range merged {
			data[mk] = mv
		}
		return data
	}
	data[key] = value
	return data
}
