package filesystem_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

const tenant = "11111111-1111-4111-8111-111111111111"

func grant(t string) auth.Grant { return auth.SystemGrant("file.read", t) }

// TestTheKeyIsPrefixedByTheTenant is the property every driver inherits by
// never being told the tenant at all. Without it, tenant isolation would be
// something each implementation has to remember.
func TestTheKeyIsPrefixedByTheTenant(t *testing.T) {
	got, err := filesystem.Key(grant(tenant), "invoices/2026-08.pdf")
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if want := tenant + "/invoices/2026-08.pdf"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}

func TestAGrantWithoutATenantIsRefused(t *testing.T) {
	if _, err := filesystem.Key(grant(""), "x.pdf"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestTheZeroGrantReachesNothing: a caller who authorized nothing has nothing
// to build a path out of, and that has to be true without anyone remembering to
// call Check.
func TestTheZeroGrantReachesNothing(t *testing.T) {
	if _, err := filesystem.Key(auth.Grant{}, "x.pdf"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestAKeyCannotEscapeItsTenant is the check that matters. A key is often a
// filename that came from an upload, and "../../../etc/passwd" is what an
// upload form eventually receives.
func TestAKeyCannotEscapeItsTenant(t *testing.T) {
	for _, key := range []string{
		"../other-tenant/secret.pdf",
		"../../etc/passwd",
		"invoices/../../../etc/passwd",
		"..",
		"",
		".",
		"./",
		"a\x00b",
	} {
		if _, err := filesystem.Key(grant(tenant), key); err == nil {
			t.Errorf("%q was accepted", key)
		}
	}
}

// TestAKeyIsRejectedRatherThanRewritten: a key that had to be rewritten to be
// safe is a key the caller did not mean, and storing it somewhere else quietly
// is worse than an error.
func TestAKeyIsRejectedRatherThanRewritten(t *testing.T) {
	if _, err := filesystem.CleanKey("../escape"); !errors.Is(err, filesystem.ErrBadKey) {
		t.Fatalf("err = %v, want ErrBadKey", err)
	}
}

// TestAnAbsoluteKeyIsNotAnEscape: "/etc/passwd" becomes "<tenant>/etc/passwd",
// which is inside the prefix and harmless. Only "../" leaves, and that is what
// the rejection is for -- refusing absolute keys too would reject "/avatar.png"
// from an upload form for no reason anyone could explain.
func TestAnAbsoluteKeyIsNotAnEscape(t *testing.T) {
	got, err := filesystem.Key(grant(tenant), "/etc/passwd")
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if want := tenant + "/etc/passwd"; got != want {
		t.Fatalf("key = %q, want %q -- inside the tenant prefix", got, want)
	}
}

// TestHarmlessKeysStillWork: normalizing has to leave ordinary keys alone, or
// people start working around it.
func TestHarmlessKeysStillWork(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"file.pdf", "file.pdf"},
		{"/file.pdf", "file.pdf"},
		{"a/b/c.pdf", "a/b/c.pdf"},
		{"a//b.pdf", "a/b.pdf"},
		{"a/./b.pdf", "a/b.pdf"},
		{"a/b/../c.pdf", "a/c.pdf"},
	} {
		got, err := filesystem.CleanKey(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q became %q, want %q", c.in, got, c.want)
		}
	}
}

// TestOneTenantCannotBuildAnotherTenantsKey: the prefix comes from the Grant
// and never from the key, which is what makes the isolation unbypassable rather
// than conventional.
func TestOneTenantCannotBuildAnotherTenantsKey(t *testing.T) {
	other := "22222222-2222-4222-8222-222222222222"

	mine, err := filesystem.Key(grant(tenant), "invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := filesystem.Key(grant(other), "invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if mine == theirs {
		t.Fatal("two tenants produced the same path for the same key")
	}

	// And naming the other tenant in the key does not reach it.
	attempt, err := filesystem.Key(grant(tenant), other+"/invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if attempt == theirs {
		t.Fatal("a key naming another tenant reached that tenant's path")
	}
}

// TestATenantCannotBeAPathItself is the hole an audit found: the key was
// checked and the tenant was not.
//
// Tenant "acme/reports" storing "q1.pdf" and tenant "acme" storing
// "reports/q1.pdf" resolved to the same object, each holding a perfectly valid
// Grant of its own. No Policy was violated -- the path is built after the
// Policy runs, which is exactly why this check has to live in Key.
func TestATenantCannotBeAPathItself(t *testing.T) {
	for _, tenant := range []string{
		"acme/reports", // collides with tenant "acme" storing "reports/..."
		"../../etc",    // leaves the prefix entirely
		"a:b",          // a separator in Redis keys
		"a\x00b",       // truncates in every syscall that takes a path
		"..",
		"/",
		"a b",
	} {
		if _, err := filesystem.Key(grant(tenant), "file.pdf"); err == nil {
			t.Errorf("tenant %q was accepted as a path segment", tenant)
		}
	}
}

// TestTwoTenantsNeverResolveToTheSameObject: the collision itself, written down.
func TestTwoTenantsNeverResolveToTheSameObject(t *testing.T) {
	// Both are refused now, but the assertion is about the outcome rather than
	// the mechanism: if a future change lets one through, this fails.
	first, errFirst := filesystem.Key(grant("acme/reports"), "q1.pdf")
	second, errSecond := filesystem.Key(grant("acme"), "reports/q1.pdf")

	if errFirst == nil && errSecond == nil && first == second {
		t.Fatalf("two tenants resolved to the same object: %q", first)
	}
}

// TestAnOrdinaryTenantStillWorks: the check has to let through what a real
// application uses, or people work around it.
func TestAnOrdinaryTenantStillWorks(t *testing.T) {
	for _, tenant := range []string{
		"11111111-1111-4111-8111-111111111111", // a UUID
		"acme",                                 // a slug
		"acme-brasil",
		"tenant_42",
		"42",
	} {
		if _, err := filesystem.Key(grant(tenant), "file.pdf"); err != nil {
			t.Errorf("tenant %q was refused: %v", tenant, err)
		}
	}
}
