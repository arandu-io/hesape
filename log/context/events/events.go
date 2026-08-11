package events

// Repository is the slice of Illuminate\Log\Context\Repository that these two
// events carry.
//
// The events name the concrete repository in PHP. Here they name an interface
// instead, for one reason: log/context dispatches them, so an event that named
// *context.Repository would import the package that imports it. A listener that
// wants the whole repository asserts back to it, which is what
// Repository.Dehydrating and Repository.Hydrated do for you.
type Repository interface {
	// All answers Illuminate\Log\Context\Repository::all.
	All() map[string]any

	// AllHidden answers Illuminate\Log\Context\Repository::allHidden.
	AllHidden() map[string]any
}

// ContextDehydrating answers Illuminate\Log\Context\Events\ContextDehydrating.
//
// It is dispatched while the context is being written down to travel with a
// queued job, before the values are serialised, so a listener may still add to
// it or take something out. The repository it carries is a copy taken for the
// dehydration, not the live one, exactly as Illuminate builds a new instance
// before dispatching.
type ContextDehydrating struct {
	// Context is the context instance.
	Context Repository
}

// ContextHydrated answers Illuminate\Log\Context\Events\ContextHydrated.
//
// It is dispatched after a dehydrated context was read back, on the other side
// of the queue, with the values already restored.
type ContextHydrated struct {
	// Context is the context instance.
	Context Repository
}
