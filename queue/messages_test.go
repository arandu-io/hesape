package queue_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/queue"
)

// saysWhereTheFixGoes fails unless msg names every one of want and none of the
// files an application no longer has: bootstrap/ holds app.go alone and it
// builds no queue, so a message sending the reader there sends them nowhere.
func saysWhereTheFixGoes(t *testing.T, msg string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("the message does not name %q: %s", w, msg)
		}
	}
	for _, gone := range []string{"bootstrap/app.go", "main.go", "bootstrap/console.go"} {
		if strings.Contains(msg, gone) {
			t.Errorf("the message sends the reader to %s, which no longer builds the queue: %s", gone, msg)
		}
	}
}

// TestAnUnknownConnectionNamesTheSettingThatRegistersOne: the framework
// registers the one queue QUEUE_CONNECTION names, so that setting is the fix
// for an application, and Extend is the fix for a manager built by hand.
func TestAnUnknownConnectionNamesTheSettingThatRegistersOne(t *testing.T) {
	_, err := queue.NewQueueManager().Connection("redis")
	if !errors.Is(err, queue.ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
	saysWhereTheFixGoes(t, err.Error(), `"redis"`, "QUEUE_CONNECTION", "Extend")
}

// TestAPauseWithNoCacheNamesTheSettingThatWiresOne: the shared store the pause
// is written to is the one CACHE_STORE or SESSION_DRIVER resolves.
func TestAPauseWithNoCacheNamesTheSettingThatWiresOne(t *testing.T) {
	m := queue.NewQueueManager()
	m.Extend("database", queue.NullQueue{})

	err := m.Pause(context.Background(), "database", "default")
	if !errors.Is(err, queue.ErrNoCache) {
		t.Fatalf("err = %v, want ErrNoCache", err)
	}
	saysWhereTheFixGoes(t, err.Error(), "CACHE_STORE=redis", "SESSION_DRIVER=redis", "SetCache")
}

// TestAModelWithNoFinderNamesWhereFindersAreRegistered: a finder is registered
// beside the job handlers, which an application keeps in app/Providers.
func TestAModelWithNoFinderNamesWhereFindersAreRegistered(t *testing.T) {
	var models queue.SerializesModels
	_, err := models.RestoreModel(context.Background(), "invoices.view", queue.ModelIdentifier{Class: "invoice", ID: "1", TenantID: "acme"})
	if err == nil {
		t.Fatal("restoring a class nothing registered succeeded")
	}
	saysWhereTheFixGoes(t, err.Error(), `"invoice"`, "FindModelsUsing", "app/Providers")
}
