package webhook

import (
	"fmt"
	"sort"
	"testing"
)

// TestAnEndpointSubscribedBehindFiveHundredOthersStillReceivesTheEvent: the
// publisher read the first maxFanout active endpoints and only then asked
// which were subscribed, so in a tenant with more active endpoints than that,
// one subscribed endpoint whose id sorted after them was never sent the event,
// and nothing said so. It reads every page now, and caps the subscribed ones.
func TestAnEndpointSubscribedBehindFiveHundredOthersStillReceivesTheEvent(t *testing.T) {
	var all []*Endpoint
	for i := range 1203 {
		events := "other.event"
		if i >= 1200 {
			events = "order.paid"
		}
		all = append(all, &Endpoint{ID: fmt.Sprintf("ep_%05d", i), Events: events, Active: true})
	}
	page := func(after string) ([]*Endpoint, error) {
		at := sort.Search(len(all), func(i int) bool { return all[i].ID > after })
		end := min(at+fanoutPage, len(all))
		return all[at:end], nil
	}

	got, err := subscribed(page, "order.paid")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "ep_01200" || got[2].ID != "ep_01202" {
		ids := []string{}
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		t.Fatalf("subscribed = %v, want the three endpoints subscribed after 1200 that are not", ids)
	}
}

// TestTheFanoutStopsAtMaxFanoutSubscribedEndpoints: the cap is on the
// endpoints an event is sent to, not on the ones read to find them.
func TestTheFanoutStopsAtMaxFanoutSubscribedEndpoints(t *testing.T) {
	var all []*Endpoint
	for i := range maxFanout + 7 {
		all = append(all, &Endpoint{ID: fmt.Sprintf("ep_%05d", i), Events: AllEvents, Active: true})
	}
	page := func(after string) ([]*Endpoint, error) {
		at := sort.Search(len(all), func(i int) bool { return all[i].ID > after })
		return all[at:min(at+fanoutPage, len(all))], nil
	}
	got, err := subscribed(page, "order.paid")
	if err != nil || len(got) != maxFanout {
		t.Fatalf("subscribed = %d endpoints, %v, want %d", len(got), err, maxFanout)
	}
}

// TestAnUpdateToANewAddressDropsTheStoredHeaders: the stored headers are
// sealed and never shown back, and an update with no headers kept them
// whatever its URL was, so a subject allowed to update an endpoint but not to
// reveal its secrets could point it at a server of their own and read the
// header values off the next delivery.
func TestAnUpdateToANewAddressDropsTheStoredHeaders(t *testing.T) {
	record := &Endpoint{URL: "https://partner.example/hooks"}
	same := endpointInput{url: "https://partner.example/hooks"}
	moved := endpointInput{url: "https://attacker.example/hooks"}
	if !keepsHeaders(EndpointRequest{}, same, record) {
		t.Error("an update to the same address with no headers dropped the stored ones")
	}
	if keepsHeaders(EndpointRequest{}, moved, record) {
		t.Error("an update to a new address with no headers kept the stored ones")
	}
	if keepsHeaders(EndpointRequest{Headers: "Authorization: Bearer new"}, moved, record) {
		t.Error("an update that supplied headers kept the stored ones instead")
	}
}
