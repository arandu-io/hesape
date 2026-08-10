package support

import "sort"

// DefaultErrorBag answers to the 'default' key ViewErrorBag reads when no bag
// is named.
const DefaultErrorBag = "default"

// ViewErrorBag answers to Illuminate\Support\ViewErrorBag: the several named
// MessageBags a request can carry, with one of them called "default".
//
// The PHP forwards unknown calls to the default bag through __call. Go has no
// __call, so the read side of MessageBag is written out below.
type ViewErrorBag struct {
	bags map[string]*MessageBag
}

// NewViewErrorBag builds the empty bag of bags the PHP gets from `new`.
func NewViewErrorBag() *ViewErrorBag {
	return &ViewErrorBag{bags: map[string]*MessageBag{}}
}

// HasBag answers to ViewErrorBag::hasBag. An empty key stands for the PHP
// default of 'default'.
func (v *ViewErrorBag) HasBag(key string) bool {
	if key == "" {
		key = DefaultErrorBag
	}
	_, ok := v.bags[key]
	return ok
}

// GetBag answers to ViewErrorBag::getBag. A key with no bag gives an empty
// MessageBag, never nil, so a view can call First on it.
func (v *ViewErrorBag) GetBag(key string) *MessageBag {
	if key == "" {
		key = DefaultErrorBag
	}
	if bag, ok := v.bags[key]; ok && bag != nil {
		return bag
	}
	return NewMessageBag(nil)
}

// GetBags answers to ViewErrorBag::getBags.
func (v *ViewErrorBag) GetBags() map[string]*MessageBag {
	out := make(map[string]*MessageBag, len(v.bags))
	for k, bag := range v.bags {
		out[k] = bag
	}
	return out
}

// Put answers to ViewErrorBag::put.
func (v *ViewErrorBag) Put(key string, bag *MessageBag) *ViewErrorBag {
	if v.bags == nil {
		v.bags = map[string]*MessageBag{}
	}
	if key == "" {
		key = DefaultErrorBag
	}
	v.bags[key] = bag
	return v
}

// Any answers to ViewErrorBag::any: whether the default bag has messages.
func (v *ViewErrorBag) Any() bool { return v.Count() > 0 }

// Count answers to ViewErrorBag::count: the messages in the default bag.
func (v *ViewErrorBag) Count() int { return v.GetBag(DefaultErrorBag).Count() }

// String answers to ViewErrorBag::__toString.
func (v *ViewErrorBag) String() string { return v.GetBag(DefaultErrorBag).String() }

// Names lists the named bags, sorted. It stands for iterating $bags, which the
// PHP does through the array itself.
func (v *ViewErrorBag) Names() []string {
	out := make([]string, 0, len(v.bags))
	for k := range v.bags {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// First answers to MessageBag::first on the default bag, through __call.
func (v *ViewErrorBag) First(key string, format ...string) string {
	return v.GetBag(DefaultErrorBag).First(key, format...)
}

// Get answers to MessageBag::get on the default bag, through __call.
func (v *ViewErrorBag) Get(key string, format ...string) []string {
	return v.GetBag(DefaultErrorBag).Get(key, format...)
}

// All answers to MessageBag::all on the default bag, through __call.
func (v *ViewErrorBag) All(format ...string) []string {
	return v.GetBag(DefaultErrorBag).All(format...)
}

// Unique answers to MessageBag::unique on the default bag, through __call.
func (v *ViewErrorBag) Unique(format ...string) []string {
	return v.GetBag(DefaultErrorBag).Unique(format...)
}

// Has answers to MessageBag::has on the default bag, through __call.
func (v *ViewErrorBag) Has(keys ...string) bool {
	return v.GetBag(DefaultErrorBag).Has(keys...)
}

// HasAny answers to MessageBag::hasAny on the default bag, through __call.
func (v *ViewErrorBag) HasAny(keys ...string) bool {
	return v.GetBag(DefaultErrorBag).HasAny(keys...)
}

// Missing answers to MessageBag::missing on the default bag, through __call.
func (v *ViewErrorBag) Missing(keys ...string) bool {
	return v.GetBag(DefaultErrorBag).Missing(keys...)
}

// Keys answers to MessageBag::keys on the default bag, through __call.
func (v *ViewErrorBag) Keys() []string { return v.GetBag(DefaultErrorBag).Keys() }

// Messages answers to MessageBag::messages on the default bag, through __call.
func (v *ViewErrorBag) Messages() map[string][]string {
	return v.GetBag(DefaultErrorBag).Messages()
}

// IsEmpty answers to MessageBag::isEmpty on the default bag, through __call.
func (v *ViewErrorBag) IsEmpty() bool { return v.GetBag(DefaultErrorBag).IsEmpty() }

// IsNotEmpty answers to MessageBag::isNotEmpty on the default bag, through __call.
func (v *ViewErrorBag) IsNotEmpty() bool { return v.GetBag(DefaultErrorBag).IsNotEmpty() }
