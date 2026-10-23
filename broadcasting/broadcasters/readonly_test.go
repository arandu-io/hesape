package broadcasters_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/broadcasting/broadcasters"
)

type allowViewing struct{}

func (allowViewing) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// TestADriverRefusesAViewingGrant: a driver's Broadcast is reachable straight
// from a handler, so the refusal the manager makes has to hold one level down
// as well, or a view could publish into the customer's channels by naming the
// connection.
func TestADriverRefusesAViewingGrant(t *testing.T) {
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowViewing{}, viewed, "order.shipped", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertDriversRefuse(t, g, auth.ErrReadOnly)
}

// readsOnly allows everything and says none of its actions write.
type readsOnly struct{ allowViewing }

func (readsOnly) Writes(auth.Action) bool { return false }

// TestADriverRefusesAReadGrant: a driver refuses the Grant a policy issued for
// an action it says does not write, as the manager does.
func TestADriverRefusesAReadGrant(t *testing.T) {
	g, err := auth.Authorize(context.Background(), readsOnly{}, auth.Subject{ID: "bea", Tenant: "acme"}, "order.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertDriversRefuse(t, g, auth.ErrReadGrant)
}

// assertDriversRefuse broadcasts with g through every driver, wants each
// refused with want, and wants nothing written.
func assertDriversRefuse(t *testing.T, g auth.Grant, want error) {
	t.Helper()
	channels := []broadcasting.Channel{broadcasting.NewPrivateChannel("orders.17")}

	redis, connection := newRedisBroadcaster(t, "", "orders.{id}")
	var written bytes.Buffer
	drivers := map[string]broadcasting.Broadcaster{
		"redis": redis,
		"log":   broadcasters.NewLogBroadcaster(slog.New(slog.NewTextHandler(&written, nil))),
	}
	for name, driver := range drivers {
		if err := driver.Broadcast(context.Background(), g, channels, "OrderShipped", nil); !errors.Is(err, want) {
			t.Errorf("%s answered %v, want %v", name, err, want)
		}
	}
	if len(connection.arguments) != 0 || written.Len() != 0 {
		t.Fatalf("a Grant that cannot write reached a driver: redis got %v, the log got %q", connection.arguments, written.String())
	}
}
