package webhook

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
)

// EndpointResolver selects the endpoints for one stored event.
type EndpointResolver interface {
	Endpoints(context.Context, auth.Grant, events.Stored) ([]Endpoint, error)
}

// EndpointResolverFunc adapts a function to EndpointResolver.
type EndpointResolverFunc func(context.Context, auth.Grant, events.Stored) ([]Endpoint, error)

// Endpoints calls f.
func (f EndpointResolverFunc) Endpoints(ctx context.Context, g auth.Grant, event events.Stored) ([]Endpoint, error) {
	return f(ctx, g, event)
}

// Publisher sends committed outbox events through Manager.Dispatch.
type Publisher struct {
	manager  *Manager
	resolver EndpointResolver
}

// NewPublisher returns the outbox publisher over manager and resolver.
func NewPublisher(manager *Manager, resolver EndpointResolver) *Publisher {
	return &Publisher{manager: manager, resolver: resolver}
}

// Publish preserves the Stored event id and dispatches without another outbox.
func (p *Publisher) Publish(ctx context.Context, stored events.Stored) error {
	if p == nil || p.manager == nil || p.resolver == nil {
		return errors.New("webhook: publisher needs a manager and endpoint resolver")
	}
	g := auth.SystemGrant(ActionDispatch, stored.TenantID)
	if _, err := tenantFor(g); err != nil {
		return err
	}
	endpoints, err := p.resolver.Endpoints(ctx, g, stored)
	if err != nil {
		return err
	}
	return p.manager.Dispatch(ctx, g, Event{
		ID: stored.ID, Name: stored.Name, Aggregate: stored.Aggregate,
		AggregateID: stored.AggregateID, Payload: []byte(stored.Payload), OccurredAt: stored.OccurredAt,
	}, endpoints)
}

var _ events.Publisher = (*Publisher)(nil)
