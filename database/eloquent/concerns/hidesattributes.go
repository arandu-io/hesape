package concerns

// HidesAttributes answers
// Illuminate\Database\Eloquent\Concerns\HidesAttributes: which attributes are
// left out of the serialized model.
//
// It is not authorization. A hidden attribute is still loaded, still readable
// with GetAttribute, and still on the row -- what it does not do is appear in a
// response body. The thing that decides whether the row may be read at all is
// the Policy, and a password_hash kept out of JSON by this list is not a
// password_hash anybody was stopped from reading.
type HidesAttributes struct {
	hidden  []string
	visible []string
}

// GetHidden answers HidesAttributes::getHidden.
func (h *HidesAttributes) GetHidden() []string { return h.hidden }

// SetHidden answers HidesAttributes::setHidden.
func (h *HidesAttributes) SetHidden(hidden []string) { h.hidden = hidden }

// MergeHidden answers HidesAttributes::mergeHidden.
func (h *HidesAttributes) MergeHidden(hidden []string) { h.hidden = append(h.hidden, hidden...) }

// GetVisible answers HidesAttributes::getVisible.
func (h *HidesAttributes) GetVisible() []string { return h.visible }

// SetVisible answers HidesAttributes::setVisible.
func (h *HidesAttributes) SetVisible(visible []string) { h.visible = visible }

// MergeVisible answers HidesAttributes::mergeVisible.
func (h *HidesAttributes) MergeVisible(visible []string) { h.visible = append(h.visible, visible...) }

// MakeVisible answers HidesAttributes::makeVisible: it drops the attributes
// from the hidden list, and adds them to the visible one when there is one.
func (h *HidesAttributes) MakeVisible(attributes ...string) {
	h.hidden = without(h.hidden, attributes)
	if len(h.visible) > 0 {
		h.visible = append(h.visible, attributes...)
	}
}

// MakeVisibleIf answers HidesAttributes::makeVisibleIf.
func (h *HidesAttributes) MakeVisibleIf(condition bool, attributes ...string) {
	if condition {
		h.MakeVisible(attributes...)
	}
}

// MakeHidden answers HidesAttributes::makeHidden.
func (h *HidesAttributes) MakeHidden(attributes ...string) {
	h.hidden = append(h.hidden, attributes...)
}

// MakeHiddenIf answers HidesAttributes::makeHiddenIf.
func (h *HidesAttributes) MakeHiddenIf(condition bool, attributes ...string) {
	if condition {
		h.MakeHidden(attributes...)
	}
}

// IsVisible reports whether an attribute survives the two lists.
//
// The PHP performs this inside getArrayableItems: a non-empty visible list is
// an allowlist, and the hidden list is subtracted after. Naming it is what lets
// AttributesToArray ask one question per attribute instead of rebuilding both
// lists per call.
func (h *HidesAttributes) IsVisible(attribute string) bool {
	if len(h.visible) > 0 && !contains(h.visible, attribute) {
		return false
	}
	return !contains(h.hidden, attribute)
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func without(values []string, remove []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if contains(remove, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}
