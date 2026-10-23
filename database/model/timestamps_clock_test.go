package model

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/support"
)

// The timestamps a save writes are read from the framework clock, so a test
// that travels in time sees created_at and updated_at move with it instead of
// sleeping until they differ.
func TestTheTimestampsASaveWritesFollowTheFrameworkClock(t *testing.T) {
	at := time.Date(2031, time.March, 4, 5, 6, 7, 0, time.FixedZone("BRT", -3*60*60))
	support.TravelTo(at)
	t.Cleanup(support.TravelBack)

	model, conn := newUserModel()
	ok, err := newQuery(model.base()).FillAndInsert(context.Background(), grant(), []map[string]any{{"name": "Ada"}})
	if err != nil || !ok {
		t.Fatalf("FillAndInsert = %v, %v", ok, err)
	}

	stamped := 0
	for _, value := range conn.last().Bindings {
		if when, isTime := value.(time.Time); isTime {
			if !when.Equal(at) || when.Location() != time.UTC {
				t.Errorf("a timestamp was written as %v, want %v in UTC", when, at.UTC())
			}
			stamped++
		}
	}
	if stamped != 2 {
		t.Errorf("the insert carried %d timestamps, want created_at and updated_at", stamped)
	}
}
