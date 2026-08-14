package scheduling

// PendingEventAttributes is a set of attributes waiting for an event to be
// declared.
//
// It answers Illuminate\Console\Scheduling\PendingEventAttributes. It is what
// makes Schedule.Daily().Group(...) work: the frequency is named before there is
// an event to put it on, so it is held here and merged into every event the
// group declares.
//
// It embeds an Event rather than repeating the seventy frequency and attribute
// methods, which is what the PHP does with the same two traits. The event it
// embeds is never run: it is a bag of attributes with the methods already on it.
type PendingEventAttributes struct {
	*Event

	schedule *Schedule
}

// NewPendingEventAttributes is PendingEventAttributes::__construct: it returns
// an empty set of attributes.
func NewPendingEventAttributes(schedule *Schedule) *PendingEventAttributes {
	return &PendingEventAttributes{
		Event:    &Event{expression: "* * * * *", expiresAt: defaultExpiresAt},
		schedule: schedule,
	}
}

// Attributes has no Illuminate counterpart: it returns the event the attributes
// are collected on, so the frequency methods chain from a group the way they
// chain from an event. PHP gets them from two traits, and Go gets them from the
// embedded Event.
func (p *PendingEventAttributes) Attributes() *Event { return p.Event }

// Schedule returns the schedule the group belongs to.
//
// It answers what PendingEventAttributes::__call proxies to: a method the
// attributes do not have is the schedule's, and in Go the caller says so.
func (p *PendingEventAttributes) Schedule() *Schedule { return p.schedule }

// WithoutOverlapping records that the events of this group must not overlap
// themselves.
//
// It answers PendingEventAttributes::withoutOverlapping, which overrides the
// trait: the attributes have no mutex to register a skip against, so it only
// records the decision and MergeAttributes is what turns it into the skip.
func (p *PendingEventAttributes) WithoutOverlapping(expiresAt ...int) *PendingEventAttributes {
	p.withoutOverlapping = true
	p.expiresAt = defaultExpiresAt
	if len(expiresAt) > 0 && expiresAt[0] > 0 {
		p.expiresAt = expiresAt[0]
	}
	return p
}

// MergeAttributes copies what was collected onto an event.
//
// It answers PendingEventAttributes::mergeAttributes, field by field and in the
// same order: only what was actually set is copied, so an event that named its
// own timezone keeps it.
func (p *PendingEventAttributes) MergeAttributes(event *Event) {
	event.expression = p.expression
	event.repeatSeconds = p.repeatSeconds

	if p.description != "" {
		event.Name(p.description)
	}
	if p.timezone != nil {
		event.Timezone(p.timezone)
	}
	if p.user != "" {
		event.User(p.user)
	}
	if len(p.environments) > 0 {
		event.Environments(p.environments...)
	}
	if p.evenInMaintenanceMode {
		event.EvenInMaintenanceMode()
	}
	if p.withoutOverlapping {
		event.WithoutOverlapping(p.expiresAt)
	}
	if p.onOneServer {
		event.OnOneServer()
	}
	if p.runInBackground {
		event.RunInBackground()
	}
	if p.action != "" {
		event.Action(p.action)
	}
	if p.perTenant {
		event.PerTenant()
	}

	for _, filter := range p.filters {
		event.When(filter)
	}
	for _, reject := range p.rejects {
		event.Skip(reject)
	}
}
