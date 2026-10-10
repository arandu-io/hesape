package view_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/view"
)

// requestNamed is a request context carrying the application name the way the
// framework puts it there, or carrying none when name is empty.
func requestNamed(name string) *hhttp.Context {
	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	if name != "" {
		req = req.WithContext(hhttp.WithAppName(req.Context(), name))
	}
	return hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)
}

// TestNewTakesTheAppNameFromTheRequest: every page the skeleton shipped, every
// page the generator wrote and every screen a module drew had an empty brand,
// because the name was left to a controller that did not have it. The
// framework puts it on the request, and view.New is the whole of what a
// controller writes.
func TestNewTakesTheAppNameFromTheRequest(t *testing.T) {
	page := view.New(requestNamed("Billing"), "Notes")

	if page.AppName != "Billing" {
		t.Fatalf("AppName = %q, want the name on the request", page.AppName)
	}
	var layout view.Layout = page
	if got := layout.BrandName(); got != "Billing" {
		t.Fatalf("BrandName = %q, want Billing: the layout draws what New filled", got)
	}
	// The name is the brand and not the title: a page that drew one in place
	// of the other would pass a check on either alone.
	if page.Title != "Notes" {
		t.Errorf("Title = %q, want Notes", page.Title)
	}
}

// TestNewWithoutAnAppNameCarriesNone: no name on the request -- a test, a
// handler mounted outside the application -- is an empty brand, and no panic.
func TestNewWithoutAnAppNameCarriesNone(t *testing.T) {
	if got := view.New(requestNamed(""), "Notes").AppName; got != "" {
		t.Fatalf("AppName = %q, want empty", got)
	}
}

// TestAnAppNameAssignedAfterNewWins: a controller may still name one page
// differently, by assigning the field on the value New returned, as with any
// other field.
func TestAnAppNameAssignedAfterNewWins(t *testing.T) {
	page := view.New(requestNamed("Billing"), "Notes")
	page.AppName = "Billing Admin"

	if got := page.BrandName(); got != "Billing Admin" {
		t.Fatalf("BrandName = %q, want the name the controller assigned", got)
	}
}

// TestNewLeavesTheSubjectsChromeToTheController: the name is configuration;
// who is greeted and which areas are offered depend on who asks, and New
// fills none of them.
func TestNewLeavesTheSubjectsChromeToTheController(t *testing.T) {
	page := view.New(requestNamed("Billing"), "Notes")

	if page.UserName != "" || page.PanelURL != "" || page.AdminURL != "" {
		t.Fatalf("UserName, PanelURL, AdminURL = %q, %q, %q, want all empty", page.UserName, page.PanelURL, page.AdminURL)
	}
}
