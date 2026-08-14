package broadcasting_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// allowAll is a Policy that permits everything, so a test can obtain a Grant
// that is valid and still says nothing about a tenant. That combination is the
// one worth testing: the zero Grant fails auth.Grant.Check and is refused
// everywhere, while a Grant a Policy really issued passes Check and can still
// carry an empty tenant.
type allowAll struct{}

func (allowAll) Can(ctx context.Context, s auth.Subject, a auth.Action, resource string) error {
	return nil
}

// grantWithoutTenant is an authorization that happened, for a subject nobody
// gave a tenant.
func grantWithoutTenant(t *testing.T) auth.Grant {
	t.Helper()

	g, err := auth.Authorize(context.Background(), allowAll{}, auth.Subject{ID: "u-1"}, broadcasting.ChannelJoin, "")
	if err != nil {
		t.Fatalf("the policy refused: %v", err)
	}
	if err := g.Check(broadcasting.ChannelJoin); err != nil {
		t.Fatalf("the grant does not pass its own Check, so this test proves nothing: %v", err)
	}

	return g
}

// RULE 14. The tenant in a published channel name comes off the Grant, and two
// tenants asking for the same channel are on two channels.
func TestTheTenantInAChannelNameComesFromTheGrant(t *testing.T) {
	channel := broadcasting.NewPrivateChannel("orders.17")

	acme, err := broadcasting.TenantChannel(auth.SystemGrant(broadcasting.ChannelJoin, "acme"), channel)
	if err != nil {
		t.Fatalf("acme could not name its own channel: %v", err)
	}
	globex, err := broadcasting.TenantChannel(auth.SystemGrant(broadcasting.ChannelJoin, "globex"), channel)
	if err != nil {
		t.Fatalf("globex could not name its own channel: %v", err)
	}

	if acme != "acme:private-orders.17" {
		t.Errorf("acme publishes on %q, want the tenant in front", acme)
	}
	if acme == globex {
		t.Fatalf("both tenants publish on %q, so the first subscriber reads the other's events", acme)
	}
}

// A Grant with no tenant names no channel -- and the interesting half is the
// second one, a Grant a Policy really issued for a subject nobody scoped.
func TestAGrantWithNoTenantNamesNoChannel(t *testing.T) {
	channel := broadcasting.NewPrivateChannel("orders.17")

	if _, err := broadcasting.TenantChannel(auth.Grant{}, channel); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("the zero Grant named a channel, error was %v", err)
	}
	if _, err := broadcasting.TenantChannel(grantWithoutTenant(t), channel); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("an authorized subject with no tenant named a channel, error was %v", err)
	}
	if _, err := broadcasting.TenantChannels(grantWithoutTenant(t), []broadcasting.Channel{channel}); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("TenantChannels named a channel without a tenant, error was %v", err)
	}
}

// A tenant that could be read as a separator is not a tenant, because the name
// it builds lands inside somebody else's namespace.
func TestATenantThatIsNotANamespaceNamesNoChannel(t *testing.T) {
	// SystemGrant already refuses these, so the Grant is built by a Policy to
	// reach the check inside TenantChannel itself.
	g, err := auth.Authorize(context.Background(), allowAll{}, auth.Subject{ID: "u-1", Tenant: "acme:globex"}, broadcasting.ChannelJoin, "")
	if err != nil {
		t.Fatalf("the policy refused: %v", err)
	}

	if _, err := broadcasting.TenantChannel(g, broadcasting.NewPrivateChannel("orders.17")); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("the tenant %q named a channel, error was %v", "acme:globex", err)
	}
}

// The tenant is added once. A name that already carries one is refused rather
// than prefixed a second time.
func TestAChannelNameMayNotCarryATenant(t *testing.T) {
	g := auth.SystemGrant(broadcasting.ChannelJoin, "globex")

	if _, err := broadcasting.TenantChannel(g, broadcasting.NewChannel("acme:private-orders.17")); !errors.Is(err, broadcasting.ErrTenantInChannelName) {
		t.Errorf("a name carrying a tenant was prefixed with a second one, error was %v", err)
	}
	if _, err := broadcasting.TenantChannel(g, broadcasting.NewChannel("")); err == nil {
		t.Error("an empty channel is a channel")
	}
}

// RequestedChannel is where the one untrusted string in this package arrives. A
// client that names a tenant is refused: naming the tenant is choosing whose
// events you hear (RULE 14).
func TestAClientMayNotNameATenant(t *testing.T) {
	requested, err := broadcasting.RequestedChannel("private-orders.17")
	if err != nil {
		t.Fatalf("an ordinary channel name was refused: %v", err)
	}
	if requested.Name != "private-orders.17" {
		t.Errorf("the requested channel is %q, want it unchanged", requested.Name)
	}

	for _, name := range []string{"acme:private-orders.17", "acme:orders.17", ""} {
		if _, err := broadcasting.RequestedChannel(name); err == nil {
			t.Errorf("the client asked for %q and was not refused", name)
		}
	}
}

// CutTenant is the reading side of TenantChannel, and the two agree on every
// name either of them can build.
func TestCutTenantIsTheInverseOfTenantChannel(t *testing.T) {
	channel := broadcasting.NewPrivateChannel("orders.17")

	published, err := broadcasting.TenantChannel(auth.SystemGrant(broadcasting.ChannelJoin, "acme"), channel)
	if err != nil {
		t.Fatalf("could not name the channel: %v", err)
	}

	tenant, name, found := broadcasting.CutTenant(published)
	if !found || tenant != "acme" || name != channel.Name {
		t.Errorf("CutTenant(%q) = (%q, %q, %t), want (acme, %q, true)", published, tenant, name, found, channel.Name)
	}

	// A name with no tenant in front comes back unchanged, which is the shape
	// of every name a client sends.
	tenant, name, found = broadcasting.CutTenant(channel.Name)
	if found || tenant != "" || name != channel.Name {
		t.Errorf("CutTenant(%q) = (%q, %q, %t), want (\"\", %q, false)", channel.Name, tenant, name, found, channel.Name)
	}

	// The left half has to be a tenant, so a name that merely contains a colon
	// is not read as one.
	if _, _, found := broadcasting.CutTenant("not a tenant:orders.17"); found {
		t.Error("a colon in the middle of a name was read as a tenant")
	}
}
