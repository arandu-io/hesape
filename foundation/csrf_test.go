package foundation_test

import (
	"slices"
	"testing"

	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/routing"
)

// receiving is a module whose routes another system calls with no session.
type receiving struct {
	name  string
	paths []string
}

func (m receiving) Name() string              { return m.name }
func (receiving) Routes(*routing.Router)      {}
func (m receiving) CSRFExemptPaths() []string { return m.paths }

// plain declares no exemption.
type plain struct{}

func (plain) Name() string           { return "plain" }
func (plain) Routes(*routing.Router) {}

var _ foundation.CSRFExempt = receiving{}

// TestCSRFExemptionsCollectsWhatTheModulesDeclare: in registration order, a
// prefix two modules share listed once, and a module that declares nothing
// contributing nothing.
func TestCSRFExemptionsCollectsWhatTheModulesDeclare(t *testing.T) {
	got := foundation.CSRFExemptions(
		receiving{"webhook", []string{"/webhooks/", "/integrations/lpr/"}},
		plain{},
		receiving{"billing", []string{"/billing/callback/", "/webhooks/"}},
	)
	want := []string{"/webhooks/", "/integrations/lpr/", "/billing/callback/"}
	if !slices.Equal(got, want) {
		t.Fatalf("CSRFExemptions = %v, want %v", got, want)
	}
	if got := foundation.CSRFExemptions(plain{}); len(got) != 0 {
		t.Fatalf("a module with no exemption contributed %v", got)
	}
}
